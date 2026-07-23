package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	sandboxsession "genesis-agent/internal/capabilities/sandbox/session"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	"genesis-agent/internal/capabilities/skill/script/materialize"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/platform/logger"
	multiagentmodel "genesis-agent/internal/runtime/multiagent/model"
	multiresult "genesis-agent/internal/runtime/multiagent/result"
)

// Service 编排 Skill 命令 materialize、工作空间与执行。
type Service struct {
	skills                skillcontract.Service
	runner                execcontract.ExecutionRunner
	approval              approvalcontract.Service
	workspaceRef          sandboxcontract.WorkspaceRef
	log                   logger.Logger
	pythonBin             string
	nodeBin               string
	enablePreflight       bool
	autoRetryAfterInstall bool
	installer             DependencyInstaller
	provisioner           workcontract.Provisioner
	inputSnapshots        workcontract.InputSnapshotReader
	producedResources     workcontract.ProducedResourceRegistrar
	sessionManager        *sandboxsession.Manager
	ownsSessionManager    bool
	reservations          artifactcontract.OutputReservationAllocator
	deliverables          artifactcontract.DeliverableSpecStore

	mu           sync.Mutex
	entries      map[string]map[string]struct{}
	materialized map[string]map[string]bool
	closed       bool
	closeOnce    sync.Once
	closeErr     error
}

// Deps 是 Skill Service 依赖。
type Deps struct {
	Skills                skillcontract.Service
	Runner                execcontract.ExecutionRunner
	Approval              approvalcontract.Service
	SessionClient         sandboxcontract.SessionClient
	FileClient            sandboxcontract.FileSystemClient
	WorkspaceRef          sandboxcontract.WorkspaceRef
	Logger                logger.Logger
	EnablePreflight       bool
	AutoRetryAfterInstall bool
	Installer             DependencyInstaller
	Provisioner           workcontract.Provisioner
	InputSnapshots        workcontract.InputSnapshotReader
	ProducedResources     workcontract.ProducedResourceRegistrar
	RemoteSessions        RemoteSessionBinder
	SessionManager        *sandboxsession.Manager
	Reservations          artifactcontract.OutputReservationAllocator
	Deliverables          artifactcontract.DeliverableSpecStore
}

// RemoteSessionBinder 在远程 session 创建后持久绑定纯数据 WorkspaceRef 与权威 lease。
type RemoteSessionBinder = sandboxcontract.RemoteSessionBinder

// ExecutionSessionStore 持久化逻辑执行会话到 durable workspace 的映射。
// 它只保存 workspace_id，不保存短命的容器/session_id，因此进程重启后可挂载新容器恢复状态。
type ExecutionSessionStore = sandboxcontract.ExecutionSessionStore

// DependencyInstaller 是可选的同回合安装端口。
type DependencyInstaller interface {
	InstallRuntime(ctx context.Context, skill string, missing []scriptcontract.MissingDep) error
}

func New(deps Deps) (*Service, error) {
	if deps.Skills == nil {
		return nil, fmt.Errorf("skill service未配置")
	}
	if deps.Runner == nil {
		return nil, fmt.Errorf("execution runner未配置")
	}
	if deps.Approval == nil {
		return nil, fmt.Errorf("approval service未配置")
	}
	if deps.Provisioner == nil {
		return nil, fmt.Errorf("workspace provisioner未配置")
	}
	log := deps.Logger
	if log == nil {
		log = logger.NewNop()
	}
	service := &Service{
		skills:                deps.Skills,
		runner:                deps.Runner,
		approval:              deps.Approval,
		workspaceRef:          deps.WorkspaceRef,
		log:                   log,
		pythonBin:             "python",
		nodeBin:               "node",
		enablePreflight:       deps.EnablePreflight,
		autoRetryAfterInstall: deps.AutoRetryAfterInstall,
		installer:             deps.Installer,
		provisioner:           deps.Provisioner,
		inputSnapshots:        deps.InputSnapshots,
		producedResources:     deps.ProducedResources,
		sessionManager:        deps.SessionManager,
		reservations:          deps.Reservations,
		deliverables:          deps.Deliverables,
		entries:               make(map[string]map[string]struct{}),
		materialized:          make(map[string]map[string]bool),
	}
	if service.sessionManager == nil && deps.SessionClient != nil && deps.FileClient != nil && deps.RemoteSessions != nil {
		manager, err := sandboxsession.NewManager(sandboxsession.ManagerDeps{
			Sessions: deps.SessionClient, Files: deps.FileClient, Workspace: deps.WorkspaceRef,
			Store: deps.RemoteSessions, Logger: log,
		})
		if err != nil {
			return nil, err
		}
		service.sessionManager = manager
		service.ownsSessionManager = true
	}
	return service, nil
}

// Close 释放 Service 缓存的远端 sandbox session。
func (s *Service) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		// 先阻止新执行进入，再等待已经登记的执行完成；Wait 与 Add 由 closed+mu
		// 建立顺序，避免关闭 Session 时仍有命令使用它。
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		if s.ownsSessionManager && s.sessionManager != nil {
			s.closeErr = s.sessionManager.Close(ctx)
		}

		s.mu.Lock()
		s.entries = make(map[string]map[string]struct{})
		s.materialized = make(map[string]map[string]bool)
		s.mu.Unlock()
	})
	return s.closeErr
}

// ReleaseRun 只释放 Run 对逻辑执行会话的活跃引用。
// 容器保留到空闲 TTL，Workspace 则独立持久化，保证连续对话不会在每轮结束时丢失路径和文件状态。
func (s *Service) ReleaseRun(ctx context.Context, prepared workmodel.PreparedRun) {
	if s == nil || strings.TrimSpace(prepared.Manifest.RunID) == "" {
		return
	}
	s.mu.Lock()
	if s.sessionManager != nil {
		s.sessionManager.ReleaseRunID(prepared.Manifest.RunID)
	}
	prefix := prepared.Manifest.RunID + "::"
	for key := range s.entries {
		if strings.HasPrefix(key, prefix) {
			delete(s.entries, key)
		}
	}
	s.mu.Unlock()
}

func (s *Service) Run(ctx context.Context, req scriptcontract.RunRequest) (*scriptcontract.RunResult, error) {
	started := time.Now()
	skillName := strings.TrimSpace(req.Skill)
	command := strings.TrimSpace(req.Command)
	if skillName == "" || command == "" {
		return nil, fmt.Errorf("skill与command不能为空")
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return &scriptcontract.RunResult{
			OK: false, Skill: skillName, Command: command,
			Error: "sandbox_unavailable: skill script service 已关闭", FailureKind: "sandbox_unavailable",
			DurationMS: time.Since(started).Milliseconds(),
		}, nil
	}
	req.Command = command
	if prefix := findLogicalPrefixInCommand(command); prefix != "" {
		out := &scriptcontract.RunResult{
			OK:              false,
			Skill:           skillName,
			Command:         command,
			Error:           errCommandLogicalPrefix(command, prefix).Error(),
			FailureKind:     "command_logical_prefix_forbidden",
			Retryable:       true,
			SuggestedAction: "stage_via_inputs_then_relative_command",
			DurationMS:      time.Since(started).Milliseconds(),
		}
		classifyFailure(out)
		return out, nil
	}
	if risk, risky := detectRiskyInlineCommand(command); risky {
		if rewrittenCmd, scriptName, payload, ok := autoRewriteRiskyInlineCommand(command); ok {
			req.Command = rewrittenCmd
			req.AutoScriptFile = &scriptcontract.AutoScriptPayload{
				Name:    scriptName,
				Content: []byte(payload),
			}
			command = rewrittenCmd
			s.log.Info("已自动将高风险内联脚本重写为隐式脚本文件执行", "script", scriptName, "rewritten", rewrittenCmd)
		} else {
			out := &scriptcontract.RunResult{
				OK:              false,
				Skill:           skillName,
				Command:         command,
				Error:           errCommandInlineRisky(command, risk).Error(),
				FailureKind:     "command_inline_risky",
				Retryable:       true,
				SuggestedAction: "write_workdir_script_then_run_relative",
				DurationMS:      time.Since(started).Milliseconds(),
				Warnings: []string{
					"python -c / node -e 多行或长串内联在 Windows 与远程 shell 下极易因引号失败；请写入 $WORK_DIR 脚本后执行。",
				},
			}
			classifyFailure(out)
			return out, nil
		}
	}
	scriptHint := commandScriptHint(command)
	if scriptHint != "" && !isExecutableSkillEntry(scriptHint) {
		out := &scriptcontract.RunResult{OK: false, Skill: skillName, Script: scriptHint, Command: command, Error: "禁止将辅助模块作为入口执行（如 path_contract.py）；请改用业务脚本入口"}
		classifyFailure(out)
		return out, nil
	}
	resolved, err := s.skills.Resolve(ctx, skillcontract.ResolveRequest{CatalogRequest: req.Catalog, Name: skillName})
	if err != nil {
		return nil, err
	}
	meta := resolved.Physical.Metadata
	invocation := req.Invocation
	if invocation.ID == "" {
		if contextual, ok := skillcontract.InvocationBindingFromContext(ctx); ok {
			invocation = contextual
		}
	}
	if invocation.ID == "" {
		invocation, err = s.skills.GetBinding(ctx, skillcontract.BindingLookup{TenantID: req.Binding.Owner.TenantID, RunID: req.Binding.Owner.RunID, Handle: skillName})
		if err != nil {
			return nil, fmt.Errorf("SKILL_BINDING_REQUIRED: run_skill_command必须在已解析InvocationBinding内执行: %w", err)
		}
	}
	if invocation.Package.Digest != resolved.Physical.Snapshot.Digest || invocation.PhysicalSkill != meta.Name {
		return nil, fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: command请求与固定Skill包不一致")
	}
	req.Invocation = invocation
	if invocation.AgentMode.Mode == skillmodel.AgentModeFork && req.Binding.Owner.ParentRunID == "" {
		out := &scriptcontract.RunResult{
			OK:          false,
			Skill:       meta.Name,
			Command:     command,
			Error:       fmt.Sprintf("FORBIDDEN_FORK_SKILL_EXECUTION: Invocation %q必须在隔离子Run执行；请调用Skill(skill=%q, task=...)", invocation.Handle, invocation.Handle),
			FailureKind: "forbidden_fork_execution",
			DurationMS:  time.Since(started).Milliseconds(),
		}
		classifyFailure(out)
		return out, nil
	}
	if scriptHint != "" {
		available, checkErr := s.commandEntryAvailable(ctx, req, meta, req.Binding.Owner.RunID, scriptHint)
		if checkErr != nil {
			return nil, fmt.Errorf("预检 Skill 命令入口: %w", checkErr)
		}
		if !available {
			required := "$WORK_DIR/" + path.Base(scriptHint)
			out := &scriptcontract.RunResult{
				OK:              false,
				Skill:           meta.Name,
				Script:          scriptHint,
				Command:         command,
				Error:           "命令入口不在 Skill 包、持久执行目录或 staged inputs 中",
				FailureKind:     "input_binding_missing",
				SuggestedAction: "stage_command_entry_via_inputs",
				Retryable:       true,
				RequiredInputs:  []string{required},
				DurationMS:      time.Since(started).Milliseconds(),
			}
			if len(req.Inputs.Inputs) == 0 && path.Dir(normalizeCommandEntry(scriptHint)) == "." {
				arguments := map[string]any{"skill": meta.Name, "command": command, "inputs": []string{required}}
				if req.TimeoutMS > 0 {
					arguments["timeout_ms"] = req.TimeoutMS
				}
				out.ExactCall = &scriptcontract.ToolCallSuggestion{Tool: "run_skill_command", Arguments: arguments}
			}
			return out, nil
		}
	}
	if reason, redundant := redundantRuntimeProbe(command, invocation.RuntimeProfile.Dependencies.Runtime); redundant {
		out := &scriptcontract.RunResult{
			OK:              false,
			Skill:           meta.Name,
			Script:          scriptHint,
			Command:         command,
			Error:           reason,
			FailureKind:     "runtime_probe_unnecessary",
			Retryable:       true,
			SuggestedAction: "run_declared_business_command_directly",
			DurationMS:      time.Since(started).Milliseconds(),
			Warnings:        []string{"Skill Harness 会在正式命令执行前校验声明的 runtime/profile；不要用独立版本或 import/require 探测消耗迭代与审批。"},
		}
		classifyFailure(out)
		return out, nil
	}
	sandbox := req.Sandbox
	if invocation.ExecutionPolicy.SandboxRequired && (sandbox.Mode == "" || sandbox.Mode == execmodel.SandboxDisabled) {
		return nil, fmt.Errorf("SKILL_RUNTIME_PROFILE_UNAVAILABLE: invocation %q要求沙箱，当前执行配置为disabled", invocation.Handle)
	}
	sandbox.TaskType = resolveTaskType(invocation.RuntimeProfile.Dependencies.Runtime)
	sandbox.Operation = execmodel.SandboxOperationRunSkill
	sandbox.RuntimeProfile = resolveRuntimeProfile(sandbox.TaskType)
	if sandbox.Metadata == nil {
		sandbox.Metadata = map[string]string{}
	}
	sandbox.Metadata["source"] = "skill"
	sandbox.Metadata["skill_id"] = meta.Name
	sandbox.Metadata["skill_package"] = string(meta.PackageID)
	if blocked, ok := detectDependencyInstallCommand(command); ok {
		out := &scriptcontract.RunResult{
			OK:          false,
			Skill:       meta.Name,
			Script:      scriptHint,
			Command:     command,
			Error:       "run_skill_command 不执行依赖安装命令；运行期脚本必须使用已声明 runtime/profile，缺包时走 install_skill_dependencies 或重建 profile",
			FailureKind: "dependency_install_forbidden",
			Missing:     installCommandMissingDeps(blocked),
			Retryable:   false,
			Metadata: map[string]string{
				"runtime_profile": string(sandbox.RuntimeProfile),
				"task_type":       string(sandbox.TaskType),
				"blocked_command": "dependency_install",
			},
			DurationMS: time.Since(started).Milliseconds(),
		}
		if useRemoteSession(sandbox) {
			out.SuggestedAction = "skip_install_and_run_with_profile_preinstalled_dependencies"
			out.Warnings = append(out.Warnings, "远程 sandbox 运行期无网络；runtime 依赖必须由 profile/镜像预装。请直接执行业务脚本，若仍缺包则修 profile。")
		} else {
			out.SuggestedAction = "use_install_skill_dependencies_or_preinstall_profile"
			out.SuggestedInstall = buildSuggestedInstall(meta.Name, out.Missing)
		}
		classifyFailureForSkill(out, invocation.RuntimeProfile.Dependencies)
		return out, nil
	}
	if err := req.Binding.Validate(); err != nil {
		return nil, fmt.Errorf("无效 Skill execution binding: %w", err)
	}
	runID := req.Binding.Owner.RunID

	approvalDisplay := meta.Name
	approvalMetadata := map[string]string{"skill": meta.Name, "command": command}
	if len(req.Inputs.Inputs) > 0 {
		approvalDisplay = fmt.Sprintf("%s（stage %d 个输入）", meta.Name, len(req.Inputs.Inputs))
		ids := make([]string, 0, len(req.Inputs.Inputs))
		for _, input := range req.Inputs.Inputs {
			ids = append(ids, input.Source.Authority+":"+input.Source.ID+"@"+input.Source.Version)
		}
		approvalMetadata["input_refs"] = strings.Join(ids, "\n")
	}
	approvalStarted := time.Now()
	decision, err := s.approval.Authorize(ctx, approvalmodel.Request{
		ToolName:        "run_skill_command",
		Action:          approvalmodel.ActionCommandExec,
		Resource:        approvalmodel.Resource{Type: "skill_command", URI: meta.Name, Display: approvalDisplay},
		Reason:          "执行 Skill 命令；输入文件将在批准后 stage 到受控工作目录",
		Risk:            approvalmodel.RiskMedium,
		SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce, approvalmodel.GrantScopeSession},
		Metadata:        approvalMetadata,
	})
	approvalDurationMS := time.Since(approvalStarted).Milliseconds()
	if err != nil {
		return nil, err
	}
	if decision.Type != approvalmodel.DecisionApproved && decision.Type != approvalmodel.DecisionApprovedForScope {
		out := &scriptcontract.RunResult{OK: false, Skill: meta.Name, Command: command, Error: fmt.Sprintf("approval %s: %s", decision.Type, decision.Reason), ApprovalDurationMS: approvalDurationMS, DurationMS: time.Since(started).Milliseconds()}
		classifyFailure(out)
		return out, nil
	}

	timeout := 120 * time.Second
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS) * time.Millisecond
	}
	out := &scriptcontract.RunResult{
		OK:                 false,
		Skill:              meta.Name,
		Script:             commandScriptHint(command),
		Command:            command,
		ApprovalDurationMS: approvalDurationMS,
		Metadata: map[string]string{
			"runtime_profile": string(sandbox.RuntimeProfile),
			"task_type":       string(sandbox.TaskType),
		},
	}

	if s.enablePreflight && !useRemoteSession(sandbox) {
		if miss := s.preflightRuntime(ctx, invocation.RuntimeProfile.Dependencies.Runtime, out.Script, req.ProjectDir); len(miss) > 0 {
			installable := installableMissing(miss)
			if len(installable) == 0 {
				for _, m := range miss {
					if m.Manager == "system" {
						cmd := m.Require
						if cmd == "" {
							cmd = m.Name
						}
						out.Warnings = append(out.Warnings, "preflight: system binary missing on PATH/image: "+cmd)
					}
				}
			} else {
				out.FailureKind = "dependency_missing"
				out.Missing = miss
				out.Error = "preflight: dependency_missing"
				out.SuggestedAction = "install_then_retry"
				out.Retryable = true
				out.SuggestedInstall = buildSuggestedInstall(meta.Name, miss)
				classifyFailure(out)
				if !s.tryAutoRetryInstall(ctx, meta.Name, out) {
					out.DurationMS = time.Since(started).Milliseconds()
					return out, nil
				}
			}
		}
	}

	for attempt := 0; attempt < 2; attempt++ {
		intendedRemote := useRemoteSession(sandbox)
		var execution skillExecutionAttempt
		if intendedRemote {
			execution = s.runRemote(ctx, meta, runID, req, timeout, sandbox)
		} else {
			execution = s.runLocal(ctx, meta, runID, req, timeout, sandbox)
		}
		s.noteKnownEntries(entryKey(runID, req.Binding.ID, meta.Name), execution.Staged, execution.Produced)
		result, err := execution.Result, execution.Err
		out.StagingDurationMS += execution.StagingDurationMS
		if result != nil {
			out.ExecutionDurationMS += result.DurationMS
		}
		degraded := result != nil && result.Environment == execmodel.EnvironmentLocal && sandbox.Mode != execmodel.SandboxDisabled
		executionBackend := resolveExecutionBackend(sandbox, intendedRemote, degraded)
		attachExecutionPathContext(out, executionBackend, degraded)
		out.WorkDir = execution.WorkDir
		out.SkillDir = execution.WorkDir
		// 按路径命名空间投影：/workspace 保留；宿主 abs（含远程 optional 降级到本地）相对化。
		// 本地平台沙箱与无沙箱同属宿主工作区命名空间。
		if !isSandboxNamespacePath(execution.WorkDir) {
			out.WorkDir = projectHostWorkDirsForModel(req.ProjectDir, execution.WorkDir)
			out.SkillDir = out.WorkDir
		}
		out.Produced = s.projectProducedCandidates(ctx, req.Binding.Owner.TenantID, runID, execution.Descriptors)
		out.StagedInputs = append([]string(nil), execution.Staged...)
		if len(execution.Warnings) > 0 {
			out.Warnings = append(out.Warnings, execution.Warnings...)
		}
		if err != nil {
			out.Error = err.Error()
			classifyFailure(out)
			if attempt == 0 && s.tryAutoRetryInstall(ctx, meta.Name, out) {
				continue
			}
			out.DurationMS = time.Since(started).Milliseconds()
			return out, nil
		}
		if result == nil {
			out.Error = "execution runner返回空结果"
			classifyFailure(out)
			out.DurationMS = time.Since(started).Milliseconds()
			return out, nil
		}
		out.ExitCode = result.ExitCode
		out.Stdout = result.Stdout
		out.Stderr = result.Stderr
		if len(result.Warnings) > 0 {
			out.Warnings = append(out.Warnings, result.Warnings...)
		}
		out.OK = result.ExitCode == 0 && !result.TimedOut && len(result.SandboxViolations) == 0
		out.Error = ""
		out.FailureKind = ""
		if result.TimedOut {
			out.Error = "skill command timed out"
			out.FailureKind = "timeout"
		}
		if len(result.SandboxViolations) > 0 {
			out.Error = "sandbox violation"
			out.FailureKind = "sandbox_violation"
			out.Warnings = append(out.Warnings, "sandbox_violations: "+strings.Join(result.SandboxViolations, "; "))
		}
		if result.ExitCode != 0 {
			out.Error = fmt.Sprintf("command exit_code=%d", result.ExitCode)
			if strings.TrimSpace(result.Error) != "" {
				out.Error = result.Error
			}
		}
		// produced 仅投影不透明候选；正式交付由 Deliverable 驱动的 Harness 决定。
		if out.OK && looksJSON(result.Stdout) {
			trimmed := strings.TrimSpace(result.Stdout)
			if strings.Contains(trimmed, `"ok":false`) {
				out.OK = false
				out.Error = "command reported ok=false"
			}
		}
		classifyFailure(out)
		if !out.OK && attempt == 0 && s.tryAutoRetryInstall(ctx, meta.Name, out) {
			continue
		}
		out.DurationMS = time.Since(started).Milliseconds()
		return out, nil
	}
	out.DurationMS = time.Since(started).Milliseconds()
	return out, nil
}

type skillExecutionAttempt struct {
	Result            *execmodel.Result
	Produced          []string
	Descriptors       []workmodel.ProducedResourceDescriptor
	WorkDir           string
	Staged            []string
	Warnings          []string
	Workspace         execmodel.ExecutionWorkspace
	StagingDurationMS int64
	Err               error
}

func (s *Service) runLocal(ctx context.Context, meta skillmodel.Metadata, runID string, req scriptcontract.RunRequest, timeout time.Duration, sandbox execmodel.SandboxProfile) skillExecutionAttempt {
	prepared, err := s.provisioner.Prepare(ctx, workcontract.PrepareRequest{StateRoot: req.StateRoot, Binding: req.Binding, Backend: req.Backend})
	if err != nil {
		return skillExecutionAttempt{Err: err}
	}
	ws := prepared.Workspace
	skillDir := filepath.Join(ws.WorkDir, "skills", sanitize(string(meta.PackageID)))
	executionWorkspace := buildSkillWorkspace(ws, skillDir, prepared.Binding.Mode)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return skillExecutionAttempt{Workspace: executionWorkspace, Err: err}
	}
	mat := &materialize.Materializer{Service: s.skills}
	if _, err := mat.MaterializePackageScripts(ctx, req.Catalog, meta, req.Invocation.Package, skillDir); err != nil {
		return skillExecutionAttempt{Workspace: executionWorkspace, Err: err}
	}
	if req.AutoScriptFile != nil && strings.TrimSpace(req.AutoScriptFile.Name) != "" {
		autoPath := filepath.Join(skillDir, req.AutoScriptFile.Name)
		if err := os.WriteFile(autoPath, req.AutoScriptFile.Content, 0o644); err != nil {
			return skillExecutionAttempt{WorkDir: skillDir, Workspace: executionWorkspace, Err: fmt.Errorf("自动写入内联脚本失败: %w", err)}
		}
	}
	stagingStarted := time.Now()
	staged, err := stageInputManifestLocal(ctx, s.inputSnapshots, req.Binding, req.Invocation.Package, req.Inputs, skillDir)
	stagingDurationMS := time.Since(stagingStarted).Milliseconds()
	if err != nil {
		return skillExecutionAttempt{WorkDir: skillDir, Workspace: executionWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	before, err := snapshotLocalFiles(skillDir)
	if err != nil {
		return skillExecutionAttempt{WorkDir: skillDir, Staged: staged, Workspace: executionWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	outputDir := executionWorkspace.OutputDir
	if strings.TrimSpace(outputDir) == "" {
		outputDir = skillDir
	}
	reserveResult, reservedEnv, err := s.prepareOutputReservations(ctx, prepared.Binding, runID, outputDir)
	if err != nil {
		return skillExecutionAttempt{WorkDir: skillDir, Staged: staged, Workspace: executionWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	if err := ensureLocalReservationDirs(outputDir, reserveResult.Reservations); err != nil {
		return skillExecutionAttempt{WorkDir: skillDir, Staged: staged, Workspace: executionWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	// cwd 钉在可写 Skill execution view；OUTPUT_DIR 必须与 reservation 物理根一致。
	baseEnv := skillEnv(skillDir, ws.TmpDir)
	baseEnv["OUTPUT_DIR"] = outputDir
	cmd := execmodel.Command{Command: req.Command, Cwd: skillDir, Shell: "auto", Env: mergeReservedEnv(baseEnv, reservedEnv)}
	result, err := s.runner.Run(ctx, cmd, execcontract.RunOptions{Timeout: timeout, Sandbox: sandbox, Binding: prepared.Binding, Workspace: executionWorkspace})
	if err != nil {
		return skillExecutionAttempt{WorkDir: skillDir, Staged: staged, Workspace: executionWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	after, err := snapshotLocalFiles(skillDir)
	if err != nil {
		return skillExecutionAttempt{Result: result, WorkDir: skillDir, Staged: staged, Workspace: executionWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	discovered := diffSnapshots(before, after)
	reservedHits := collectReservedHitsLocal(outputDir, skillDir, reserveResult.Reservations, after)
	produced := mergeProducedCandidates(reservedHits, discovered)
	produced, err = s.filterProducedByDeliverables(ctx, prepared.Binding.Owner.TenantID, runID, reservedHits, produced)
	if err != nil {
		return skillExecutionAttempt{Result: result, WorkDir: skillDir, Staged: staged, Workspace: executionWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	descriptors, err := s.registerProducedResources(ctx, runID, req.Binding, meta, produced, after, workmodel.ResourceAvailabilityDurable, nil)
	return skillExecutionAttempt{Result: result, Produced: produced, Descriptors: descriptors, WorkDir: skillDir, Staged: staged, Workspace: executionWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
}

func (s *Service) runRemote(ctx context.Context, meta skillmodel.Metadata, runID string, req scriptcontract.RunRequest, timeout time.Duration, sandbox execmodel.SandboxProfile) skillExecutionAttempt {
	remoteWorkspace := remoteSkillWorkspace(req.Binding, meta)
	if s.sessionManager == nil {
		return skillExecutionAttempt{Workspace: remoteWorkspace, Err: fmt.Errorf("sandbox_unavailable: 远程 session manager未配置")}
	}
	key := sandboxsession.ExecutionKey(ctx, req.Binding, sandbox, s.workspaceRef)
	handle, err := s.sessionManager.Acquire(ctx, sandboxsession.AcquireRequest{
		Key: key, RunID: runID, Binding: req.Binding, Workspace: remoteWorkspace, Sandbox: sandbox,
	})
	if err != nil {
		return skillExecutionAttempt{Workspace: remoteWorkspace, Err: err}
	}
	defer handle.Close()
	sess := handle.Session()
	workspaceIdentity := strings.TrimSpace(sess.Workspace().Metadata["workspace_id"])
	if workspaceIdentity == "" {
		workspaceIdentity = strings.TrimSpace(sess.Workspace().ID)
	}
	materializationKey := workspaceIdentity + "\x00" + remoteWorkspace.SkillDir + "\x00" + req.Invocation.Package.Digest
	s.mu.Lock()
	if s.materialized[key] == nil {
		s.materialized[key] = make(map[string]bool)
	}
	alreadyMaterialized := s.materialized[key][materializationKey]
	s.mu.Unlock()
	if !alreadyMaterialized {
		if err := s.materializePackageRemote(ctx, meta, req.Invocation.Package, sess, remoteWorkspace.SkillDir); err != nil {
			return skillExecutionAttempt{WorkDir: remoteWorkspace.SkillDir, Workspace: remoteWorkspace, Err: err}
		}
		s.mu.Lock()
		s.materialized[key][materializationKey] = true
		s.mu.Unlock()
	}
	stagingStarted := time.Now()
	staged, err := stageInputManifestRemote(ctx, s.inputSnapshots, req.Binding, req.Invocation.Package, req.Inputs, sess, remoteWorkspace.SkillDir)
	stagingDurationMS := time.Since(stagingStarted).Milliseconds()
	if err != nil {
		return skillExecutionAttempt{WorkDir: remoteWorkspace.SkillDir, Workspace: remoteWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	if req.AutoScriptFile != nil && strings.TrimSpace(req.AutoScriptFile.Name) != "" {
		remoteAutoPath := filepath.ToSlash(filepath.Join(remoteWorkspace.SkillDir, req.AutoScriptFile.Name))
		if err := sess.WriteFile(ctx, remoteAutoPath, req.AutoScriptFile.Content, fscontract.WriteOptions{Overwrite: true}); err != nil {
			return skillExecutionAttempt{WorkDir: remoteWorkspace.SkillDir, Workspace: remoteWorkspace, StagingDurationMS: stagingDurationMS, Err: fmt.Errorf("自动写入远程内联脚本失败: %w", err)}
		}
	}
	before, err := snapshotRemoteFiles(ctx, sess, remoteWorkspace.SkillDir)
	if err != nil {
		return skillExecutionAttempt{WorkDir: remoteWorkspace.SkillDir, Staged: staged, Workspace: remoteWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	outputDir := remoteWorkspace.OutputDir
	if strings.TrimSpace(outputDir) == "" {
		outputDir = remoteWorkspace.SkillDir
	}
	reserveResult, reservedEnv, err := s.prepareOutputReservations(ctx, req.Binding, runID, outputDir)
	if err != nil {
		return skillExecutionAttempt{WorkDir: remoteWorkspace.SkillDir, Staged: staged, Workspace: remoteWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	if err := ensureRemoteReservationDirs(ctx, sess, outputDir, reserveResult.Reservations); err != nil {
		return skillExecutionAttempt{WorkDir: remoteWorkspace.SkillDir, Staged: staged, Workspace: remoteWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	result, err := sess.Run(ctx, execmodel.Command{Command: req.Command, Cwd: remoteWorkspace.SkillDir, Shell: "auto", Env: mergeReservedEnv(remoteSkillEnv(remoteWorkspace), reservedEnv)}, execcontract.RunOptions{Timeout: timeout, Sandbox: sandbox, Binding: req.Binding, Workspace: remoteWorkspace})
	if err != nil {
		return skillExecutionAttempt{WorkDir: remoteWorkspace.SkillDir, Staged: staged, Workspace: remoteWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	after, err := snapshotRemoteFiles(ctx, sess, remoteWorkspace.SkillDir)
	if err != nil {
		return skillExecutionAttempt{Result: result, WorkDir: remoteWorkspace.SkillDir, Staged: staged, Workspace: remoteWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	discovered := diffSnapshots(before, after)
	reservedHits := collectReservedHitsRemote(ctx, sess, outputDir, remoteWorkspace.SkillDir, reserveResult.Reservations, after)
	produced := mergeProducedCandidates(reservedHits, discovered)
	produced, err = s.filterProducedByDeliverables(ctx, req.Binding.Owner.TenantID, runID, reservedHits, produced)
	if err != nil {
		return skillExecutionAttempt{Result: result, WorkDir: remoteWorkspace.SkillDir, Staged: staged, Workspace: remoteWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	// 登记前用 live Session.ExpiresAt 刷新 binding，避免心跳滑动后 produced > binding 快照。
	expiresAt := sess.ExpiresAt()
	if expiresAt.IsZero() || !expiresAt.After(time.Now()) {
		return skillExecutionAttempt{Result: result, WorkDir: remoteWorkspace.SkillDir, Staged: staged, Workspace: remoteWorkspace, StagingDurationMS: stagingDurationMS, Err: fmt.Errorf("sandbox_unavailable: 远程 session lease 已失效")}
	}
	if err := handle.RefreshBinding(ctx, req.Binding); err != nil {
		return skillExecutionAttempt{Result: result, WorkDir: remoteWorkspace.SkillDir, Staged: staged, Workspace: remoteWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
	}
	descriptors, err := s.registerProducedResources(ctx, runID, req.Binding, meta, produced, after, workmodel.ResourceAvailabilityLeased, &expiresAt)
	return skillExecutionAttempt{Result: result, Produced: produced, Descriptors: descriptors, WorkDir: remoteWorkspace.SkillDir, Staged: staged, Workspace: remoteWorkspace, StagingDurationMS: stagingDurationMS, Err: err}
}

var _ workcontract.RunResourceReleaser = (*Service)(nil)

func (s *Service) commandEntryAvailable(ctx context.Context, req scriptcontract.RunRequest, meta skillmodel.Metadata, runID, entry string) (bool, error) {
	entry = normalizeCommandEntry(entry)
	if entry == "" {
		return true, nil
	}
	if req.AutoScriptFile != nil && strings.TrimSpace(req.AutoScriptFile.Name) != "" {
		autoName := normalizeCommandEntry(req.AutoScriptFile.Name)
		if autoName == entry || path.Base(entry) == path.Base(autoName) {
			return true, nil
		}
	}
	for _, input := range req.Inputs.Inputs {
		alias := inputAlias(input)
		if normalizeCommandEntry(alias) == entry || (path.Dir(entry) == "." && path.Base(entry) == path.Base(normalizeCommandEntry(alias))) {
			return true, nil
		}
	}
	key := entryKey(runID, req.Binding.ID, meta.Name)
	s.mu.Lock()
	_, known := s.entries[key][entry]
	s.mu.Unlock()
	if known {
		return true, nil
	}
	listed, err := s.skills.ListResources(ctx, skillcontract.ListResourcesRequest{
		ResolveRequest: skillcontract.ResolveRequest{CatalogRequest: req.Catalog, Name: meta.Name, Resource: string(meta.MainResource)},
		PackageID:      meta.PackageID,
	})
	if err != nil {
		return false, err
	}
	prefix := strings.TrimSuffix(strings.ReplaceAll(string(meta.PackageID), `\`, "/"), "/") + "/"
	for _, resource := range listed.Resources {
		rel := strings.TrimPrefix(strings.ReplaceAll(string(resource.Resource), `\`, "/"), prefix)
		if normalizeCommandEntry(rel) == entry {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) noteKnownEntries(key string, staged, produced []string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[key] == nil {
		s.entries[key] = make(map[string]struct{})
	}
	for _, candidate := range append(append([]string(nil), staged...), produced...) {
		if normalized := normalizeCommandEntry(candidate); normalized != "" {
			s.entries[key][normalized] = struct{}{}
		}
	}
}

func normalizeCommandEntry(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	value = strings.TrimPrefix(value, "./")
	if value == "" {
		return ""
	}
	clean := path.Clean(value)
	if clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return ""
	}
	return clean
}

func skillEnv(workDir, tmpDir string) map[string]string {
	env := map[string]string{
		"WORK_DIR":   workDir,
		"INPUT_DIR":  workDir,
		"OUTPUT_DIR": workDir,
		"TMPDIR":     tmpDir,
		"TMP_DIR":    tmpDir,
		"SKILL_DIR":  workDir,
	}
	pyPath := filepath.ToSlash(filepath.Join(workDir, "scripts"))
	if strings.HasPrefix(workDir, "/") {
		pyPath = strings.TrimRight(workDir, "/") + "/scripts"
	}
	env["PYTHONPATH"] = pythonRuntimeSearchPath(workDir, pyPath)
	env["NODE_PATH"] = nodeRuntimeSearchPath(workDir)
	// 统一 Python 标准流为 UTF-8，减少 Windows 控制台 GBK 导致的 ToolResult 乱码。
	env["PYTHONUTF8"] = "1"
	env["PYTHONIOENCODING"] = "utf-8"
	// 宿主：优先使用 skill-deps 下 venv，使 `python -m markitdown` 走受控解释器。
	// 远程路径以 / 开头，不注入宿主机 venv（镜像预装）。
	if !strings.HasPrefix(strings.TrimSpace(workDir), "/") {
		if depRoot := skillDependencyRoot(workDir); depRoot != "" {
			venvDir := filepath.Join(depRoot, "venv")
			if venvPythonExists(venvDir) {
				env["VIRTUAL_ENV"] = venvDir
				binDir := venvBinDir(venvDir)
				path := binDir
				if existing := os.Getenv("PATH"); existing != "" {
					path = binDir + string(os.PathListSeparator) + existing
				}
				env["PATH"] = path
			}
		}
	}
	return env
}

func buildSkillWorkspace(base execmodel.ExecutionWorkspace, skillDir string, mode execmodel.WorkspaceMode) execmodel.ExecutionWorkspace {
	base.WorkDir = skillDir
	base.SkillDir = skillDir
	if mode == execmodel.WorkspaceModeSession {
		// Skill session 以相对 cwd 为唯一可变工作视图；输入通过显式 staging 进入该视图，
		// produced 也从该视图显式发布。真正的 task_job 仍保持 input/work/output 分离。
		base.InputDir = skillDir
		base.OutputDir = skillDir
	}
	return base
}

func remoteSkillWorkspace(binding execmodel.ExecutionBinding, meta skillmodel.Metadata) execmodel.ExecutionWorkspace {
	bindingID := sanitize(binding.ID)
	skillID := sanitize(string(meta.PackageID))
	workDir := "/workspace/work/" + bindingID
	skillDir := workDir + "/skills/" + skillID
	if binding.Mode == execmodel.WorkspaceModeSession {
		return execmodel.ExecutionWorkspace{
			WorkDir:   skillDir,
			InputDir:  skillDir,
			OutputDir: skillDir,
			TmpDir:    "/workspace/tmp/" + bindingID,
			SkillDir:  skillDir,
		}
	}
	return execmodel.ExecutionWorkspace{
		WorkDir:   workDir,
		InputDir:  "/workspace/input/" + bindingID,
		OutputDir: "/workspace/output/" + bindingID,
		TmpDir:    "/workspace/tmp/" + bindingID,
		SkillDir:  skillDir,
	}
}

// remoteSkillEnv 使用与宿主相同的环境变量契约，但保留远程 task job 的目录隔离。
func remoteSkillEnv(workspace execmodel.ExecutionWorkspace) map[string]string {
	env := skillEnv(workspace.SkillDir, workspace.TmpDir)
	env["WORK_DIR"] = workspace.SkillDir
	env["SKILL_DIR"] = workspace.SkillDir
	env["INPUT_DIR"] = workspace.InputDir
	env["OUTPUT_DIR"] = workspace.OutputDir
	env["TMP_DIR"] = workspace.TmpDir
	env["TMPDIR"] = workspace.TmpDir
	return env
}

func resolveTaskType(deps skillmodel.RuntimeDeps) execmodel.SandboxTaskType {
	if isOfficeRuntime(deps) {
		return execmodel.SandboxTaskOffice
	}
	return execmodel.SandboxTaskSkill
}

func resolveRuntimeProfile(taskType execmodel.SandboxTaskType) execmodel.SandboxRuntimeProfile {
	if taskType == execmodel.SandboxTaskOffice {
		return execmodel.RuntimeProfileOfficeBasic
	}
	return execmodel.RuntimeProfileSkillPolyglotBasic
}

func isOfficeRuntime(deps skillmodel.RuntimeDeps) bool {
	for _, pkg := range deps.System {
		name := strings.ToLower(strings.TrimSpace(firstNonEmpty(pkg.Command, pkg.Require, pkg.Name)))
		if strings.Contains(name, "soffice") || strings.Contains(name, "libreoffice") || strings.Contains(name, "pdftoppm") || strings.Contains(name, "poppler") {
			return true
		}
	}
	for _, pkg := range deps.Node {
		if strings.Contains(strings.ToLower(pkg.Name), "pptxgenjs") {
			return true
		}
	}
	for _, pkg := range deps.Python {
		if strings.Contains(strings.ToLower(firstNonEmpty(pkg.Name, pkg.Import)), "markitdown") {
			return true
		}
	}
	return false
}

func commandScriptHint(command string) string {
	for _, part := range strings.Fields(strings.NewReplacer("\"", "", "'", "").Replace(command)) {
		clean := strings.TrimSpace(strings.ReplaceAll(part, "\\", "/"))
		switch strings.ToLower(path.Ext(clean)) {
		case ".py", ".js", ".mjs", ".cjs", ".ts", ".ps1", ".sh":
			return clean
		}
	}
	return ""
}

func entryKey(runID, bindingID, skill string) string {
	return runID + "::" + strings.TrimSpace(bindingID) + "::" + strings.ToLower(strings.TrimSpace(skill))
}

func (s *Service) registerProducedResources(ctx context.Context, runID string, binding execmodel.ExecutionBinding, meta skillmodel.Metadata, produced []string, observed map[string]fileFingerprint, availability workmodel.ResourceAvailability, expiresAt *time.Time) ([]workmodel.ProducedResourceDescriptor, error) {
	if len(produced) == 0 {
		return nil, nil
	}
	if s.producedResources == nil {
		return nil, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("execution 已生成候选，但未装配 ProducedResourceRegistrar"))
	}
	refs := producedResourceRefs(binding, meta, produced)
	if len(refs) != len(produced) {
		return nil, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("produced 路径无法映射到稳定逻辑引用"))
	}
	descriptors := make([]workmodel.ProducedResourceDescriptor, 0, len(produced))
	base := "skills/" + sanitize(string(meta.PackageID))
	for i, candidate := range produced {
		if !safeRemoteRelativePath(candidate) {
			return nil, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("produced 路径越界: %q", candidate))
		}
		fingerprint, ok := observed[candidate]
		if !ok || fingerprint.Size < 0 {
			return nil, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("produced 候选缺少可信文件快照: %s", candidate))
		}
		observedPath, logicalRef, err := resolveProducedObservation(binding, meta, base, candidate, refs[i], availability)
		if err != nil {
			return nil, err
		}
		if err := observedPath.Validate(); err != nil {
			return nil, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, err)
		}
		descriptor, err := s.producedResources.RegisterProducedResource(ctx, workcontract.RegisterProducedResourceRequest{
			TenantID: binding.Owner.TenantID, RunID: runID, BindingID: binding.ID,
			LogicalRef: logicalRef, ObservedPath: observedPath, ObservedName: path.Base(normalizeSlash(candidate)),
			MediaType: mime.TypeByExtension(path.Ext(candidate)), Size: fingerprint.Size,
			Availability: availability, ExpiresAt: expiresAt,
		})
		if err != nil {
			return nil, fmt.Errorf("登记 produced resource %s 失败: %w", candidate, err)
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors, nil
}

// resolveProducedObservation 将 skill 视图内相对候选映射为登记用 ObservedPath/LogicalRef。
// leased（远程 session-file）：ObservedPath 必须是 /workspace 下完整相对路径（与 LogicalRef 对齐），
// 不能写成 skills/<pkg>/...，否则会落到错误的 /workspace/skills/...。
// durable（本地 Host）：ObservedPath 相对 binding WorkDir（skills/<pkg>/... 或 reserved/...）。
func resolveProducedObservation(binding execmodel.ExecutionBinding, meta skillmodel.Metadata, skillBase, candidate, fallbackLogical string, availability workmodel.ResourceAvailability) (workmodel.WorkspacePath, string, error) {
	candidate = normalizeSlash(candidate)
	logicalRef := strings.TrimSpace(fallbackLogical)
	if logicalRef == "" {
		observed, logical := producedObservation(binding, meta, skillBase, candidate, "")
		return observed, logical, nil
	}
	if availability == workmodel.ResourceAvailabilityLeased {
		if !strings.HasPrefix(logicalRef, "run:/") {
			return "", "", workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("leased produced logical_ref 无效: %q", logicalRef))
		}
		rel := strings.TrimPrefix(logicalRef, "run:/")
		if !safeRemoteRelativePath(rel) {
			return "", "", workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("leased produced ObservedPath 越界: %q", rel))
		}
		return workmodel.WorkspacePath(rel), logicalRef, nil
	}
	observed, logical := producedObservation(binding, meta, skillBase, candidate, logicalRef)
	return observed, logical, nil
}

func producedObservation(binding execmodel.ExecutionBinding, _ skillmodel.Metadata, skillBase, candidate, fallbackLogical string) (workmodel.WorkspacePath, string) {
	candidate = normalizeSlash(candidate)
	// reservation：Host 侧相对 OutputDir；逻辑引用优先使用 producedResourceRefs 结果。
	if strings.HasPrefix(candidate, "reserved/") {
		logical := fallbackLogical
		if logical == "" {
			logical = "run:/work/" + sanitize(binding.ID) + "/output/" + candidate
		}
		return workmodel.WorkspacePath(candidate), logical
	}
	if strings.HasPrefix(candidate, "output/") || strings.HasPrefix(candidate, "work/") {
		return workmodel.WorkspacePath(candidate), "run:/" + candidate
	}
	logical := fallbackLogical
	if logical == "" {
		logical = "run:/" + path.Join("work", sanitize(binding.ID), skillBase, candidate)
	}
	return workmodel.WorkspacePath(path.Join(skillBase, candidate)), logical
}

// projectProducedCandidates 投影最小候选；能唯一匹配 DeliverableSpec 时填 deliverable_id。
// 视觉 QA 预览图标记 role=qa_asset，便于模型识别且不暗示应 Delivery。
func (s *Service) projectProducedCandidates(ctx context.Context, tenantID, runID string, descriptors []workmodel.ProducedResourceDescriptor) []scriptcontract.ProducedCandidate {
	specs := s.listDeliverableSpecs(ctx, tenantID, runID)
	result := make([]scriptcontract.ProducedCandidate, 0, len(descriptors))
	for _, descriptor := range descriptors {
		candidate := scriptcontract.ProducedCandidate{CandidateID: descriptor.ID, Name: descriptor.ObservedName, MediaType: descriptor.MediaType}
		if id := uniqueMatchingDeliverableID(specs, descriptor); id != "" {
			candidate.DeliverableID = id
		} else if isQAPreviewAsset(descriptor.ObservedName) {
			candidate.Role = multiagentmodel.ArtifactRoleQAAsset
		}
		// 不再「创建即全局登记」：产物只在本 Run 内可读；跨 Run 可读由父子边界的一次显式 Adopt 授予（spec §7.2）。
		// 带上 Role：qa_asset 会在子 Run 归约时被剔除，父根本收不到。
		multiresult.RegisterArtifact(ctx, multiagentmodel.Artifact{
			CandidateID: descriptor.ID,
			ResourceID:  descriptor.ID,
			Name:        descriptor.ObservedName,
			Path:        descriptor.ObservedName,
			Kind:        "file",
			Role:        candidate.Role,
		})
		result = append(result, candidate)
	}
	return result
}

func (s *Service) listDeliverableSpecs(ctx context.Context, tenantID, runID string) []artifactmodel.DeliverableSpec {
	if s == nil || s.deliverables == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(runID) == "" {
		return nil
	}
	specs, err := s.deliverables.ListDeliverables(ctx, tenantID, runID)
	if err != nil {
		return nil
	}
	return specs
}

func uniqueMatchingDeliverableID(specs []artifactmodel.DeliverableSpec, descriptor workmodel.ProducedResourceDescriptor) string {
	matched := ""
	for _, spec := range specs {
		// 投影归属必须按观测文件名/MIME 匹配；不能用 DesiredName 自身去满足后缀约束。
		if !spec.MatchesObserved(descriptor.ObservedName, descriptor.MediaType) {
			continue
		}
		if matched != "" && matched != spec.ID {
			return ""
		}
		matched = spec.ID
	}
	return matched
}

// materializePackageRemote 直接把不可变 SkillPackageRef 投影到 executor WorkspaceFS，
// 禁止先借宿主 /workspace 或进程 cwd 建立临时副本。
func (s *Service) materializePackageRemote(ctx context.Context, meta skillmodel.Metadata, expected skillmodel.SkillPackageSnapshot, session *sandboxsession.Session, skillDir string) error {
	reader, ok := s.skills.(skillcontract.PackageSnapshotReader)
	if !ok {
		return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: skill service不支持包快照读取")
	}
	stored, files, err := reader.GetPackageSnapshot(ctx, expected.Digest)
	if err != nil {
		return fmt.Errorf("读取固定 skill package snapshot失败: %w", err)
	}
	if stored.Digest != expected.Digest || stored.PackageID != expected.PackageID || stored.Authority != expected.Authority {
		return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: package snapshot identity不一致")
	}
	prefix := string(meta.PackageID) + "/"
	scriptCount := 0
	for _, file := range files {
		rel := strings.TrimPrefix(string(file.Resource), prefix)
		if rel == "" || rel == string(file.Resource) || !safeRemoteRelativePath(rel) {
			return fmt.Errorf("skill package resource非法: %s", file.Resource)
		}
		// materialize 必须可重入：上一次上传可能只完成了部分文件，重试时以权威的
		// 不可变 SkillPackage 覆盖同路径，不能因半成品 AlreadyExists 永久卡死。
		target := sandboxsession.RelativePath(skillDir, rel)
		if err := session.WriteFile(ctx, target, file.Content, fscontract.WriteOptions{CreateParents: true, Overwrite: true, Atomic: true}); err != nil {
			return fmt.Errorf("写入远程 skill 资源 %s 失败: %w", file.Resource, err)
		}
		actual, err := session.ReadFile(ctx, target, fscontract.ReadOptions{MaxBytes: int64(len(file.Content)) + 1})
		if err != nil {
			return fmt.Errorf("校验远程 skill 资源 %s 失败: %w", file.Resource, err)
		}
		digest := sha256.Sum256(actual)
		expectedDigest := sha256.Sum256(file.Content)
		if digest != expectedDigest {
			return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: 远程materialized resource摘要不一致: %s", file.Resource)
		}
		if strings.HasPrefix(rel, "scripts/") {
			scriptCount++
		}
	}
	if scriptCount == 0 {
		return fmt.Errorf("skill %s 没有可执行 scripts", meta.Name)
	}
	return nil
}

func stageInputManifestRemote(ctx context.Context, reader workcontract.InputSnapshotReader, binding execmodel.ExecutionBinding, pkg skillmodel.SkillPackageSnapshot, manifest workmodel.InputManifest, session *sandboxsession.Session, skillDir string) ([]string, error) {
	if len(manifest.Inputs) == 0 {
		return nil, nil
	}
	if err := validateInputManifest(binding, pkg, manifest); err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("远程输入缺少 InputSnapshotReader"))
	}
	staged := make([]string, 0, len(manifest.Inputs))
	for _, input := range manifest.Inputs {
		content, err := readVerifiedSnapshot(ctx, reader, input)
		if err != nil {
			return nil, err
		}
		alias := inputAlias(input)
		if err := session.WriteFile(ctx, sandboxsession.RelativePath(skillDir, alias), content, fscontract.WriteOptions{CreateParents: true, Overwrite: true}); err != nil {
			return nil, fmt.Errorf("上传输入快照 %s 失败: %w", input.ID, err)
		}
		staged = append(staged, alias)
	}
	return staged, nil
}

func safeRemoteRelativePath(value string) bool {
	value = normalizeSlash(value)
	if value == "" || value == "." || strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func stageInputManifestLocal(ctx context.Context, reader workcontract.InputSnapshotReader, binding execmodel.ExecutionBinding, pkg skillmodel.SkillPackageSnapshot, manifest workmodel.InputManifest, destDir string) ([]string, error) {
	if len(manifest.Inputs) == 0 {
		return nil, nil
	}
	if err := validateInputManifest(binding, pkg, manifest); err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("本地输入缺少 InputSnapshotReader"))
	}
	staged := make([]string, 0, len(manifest.Inputs))
	for _, input := range manifest.Inputs {
		content, err := readVerifiedSnapshot(ctx, reader, input)
		if err != nil {
			return nil, err
		}
		alias := inputAlias(input)
		target := filepath.Join(destDir, filepath.FromSlash(alias))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return nil, err
		}
		tmp, err := os.CreateTemp(filepath.Dir(target), ".input-view-*")
		if err != nil {
			return nil, err
		}
		tmpName := tmp.Name()
		if _, err = tmp.Write(content); err == nil {
			err = tmp.Sync()
		}
		closeErr := tmp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Chmod(tmpName, 0o400)
		}
		if err == nil {
			if chmodErr := os.Chmod(target, 0o600); chmodErr != nil && !os.IsNotExist(chmodErr) {
				err = chmodErr
			}
		}
		if err == nil {
			if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
				err = removeErr
			}
		}
		if err == nil {
			err = os.Rename(tmpName, target)
		}
		if err != nil {
			_ = os.Remove(tmpName)
			return nil, fmt.Errorf("建立输入只读视图 %s 失败: %w", alias, err)
		}
		staged = append(staged, alias)
	}
	return staged, nil
}

func inputAlias(input workmodel.InputRef) string {
	return string(input.Alias)
}

func validateInputManifest(binding execmodel.ExecutionBinding, pkg skillmodel.SkillPackageSnapshot, manifest workmodel.InputManifest) error {
	if manifest.RunID != binding.Owner.RunID || manifest.BindingID != binding.ID {
		return workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("InputManifest 与 ExecutionBinding 不匹配"))
	}
	seen := make(map[string]struct{}, len(manifest.Inputs))
	packageFiles := make(map[string]struct{}, len(pkg.Files))
	prefix := string(pkg.PackageID) + "/"
	for _, file := range pkg.Files {
		rel := strings.TrimPrefix(normalizeSlash(string(file.Resource)), prefix)
		if rel != "" && rel != string(file.Resource) {
			packageFiles[strings.ToLower(rel)] = struct{}{}
		}
	}
	for _, input := range manifest.Inputs {
		if err := input.Alias.Validate(); err != nil {
			return workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("InputRef %s alias 无效: %w", input.ID, err))
		}
		key := strings.ToLower(string(input.Alias))
		if _, reserved := packageFiles[key]; reserved {
			return workcontract.NewError(workcontract.ErrCodeInputNameConflict, fmt.Errorf("InputManifest alias与不可变Skill包文件冲突: %s", input.Alias))
		}
		if _, exists := seen[key]; exists {
			return workcontract.NewError(workcontract.ErrCodeInputNameConflict, fmt.Errorf("InputManifest alias 重复: %s", input.Alias))
		}
		seen[key] = struct{}{}
	}
	return nil
}

func readVerifiedSnapshot(ctx context.Context, reader workcontract.InputSnapshotReader, input workmodel.InputRef) ([]byte, error) {
	handle, err := reader.OpenSnapshot(ctx, input.StagedPath)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	limited := io.LimitReader(handle, input.Size+1)
	content, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != input.Size {
		return nil, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("输入快照 %s 大小已变化", input.ID))
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != input.SHA256 {
		return nil, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("输入快照 %s hash 已变化", input.ID))
	}
	return content, nil
}

func snapshotLocalFiles(root string) (map[string]fileFingerprint, error) {
	out := map[string]fileFingerprint{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = fileFingerprint{Size: info.Size(), ModTime: info.ModTime().UnixNano()}
		return nil
	})
	return out, err
}

func snapshotRemoteFiles(ctx context.Context, sess *sandboxsession.Session, root string) (map[string]fileFingerprint, error) {
	rootRel := normalizeSlash(sandboxsession.RelativePath(root, ""))
	walk, err := sess.Walk(ctx, rootRel, fscontract.WalkOptions{})
	if err != nil {
		return nil, err
	}
	out := map[string]fileFingerprint{}
	if walk == nil {
		return out, nil
	}
	for _, entry := range walk.Entries {
		if entry.Type == fsmodel.EntryTypeDir {
			continue
		}
		entryPath := normalizeSlash(entry.Path)
		if !strings.HasPrefix(entryPath, rootRel+"/") {
			return nil, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("远程 Walk 返回执行目录外路径: %q", entry.Path))
		}
		rel := strings.TrimPrefix(entryPath, rootRel+"/")
		if !safeRemoteRelativePath(rel) {
			return nil, workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("远程 Walk 返回非法相对路径: %q", entry.Path))
		}
		out[rel] = fileFingerprint{Size: entry.Size, ModTime: entry.ModifiedAt.UnixNano()}
	}
	return out, nil
}

type fileFingerprint struct {
	Size    int64
	ModTime int64
}

func diffSnapshots(before, after map[string]fileFingerprint) []string {
	produced := make([]string, 0)
	for path, now := range after {
		if shouldIgnoreProducedPath(path) {
			continue
		}
		prev, ok := before[path]
		if !ok || prev != now {
			produced = append(produced, path)
		}
	}
	sort.Strings(produced)
	return produced
}

func shouldIgnoreProducedPath(rel string) bool {
	slash := strings.ToLower(filepath.ToSlash(strings.TrimSpace(rel)))
	if slash == "" {
		return true
	}
	base := filepath.Base(slash)
	if isReservedDOSDeviceName(base) {
		return true
	}
	for _, part := range strings.Split(slash, "/") {
		if isReservedDOSDeviceName(part) {
			return true
		}
	}
	if strings.Contains(slash, "/__pycache__/") || strings.HasPrefix(slash, "__pycache__/") {
		return true
	}
	if strings.Contains(slash, "/node_modules/") || strings.HasPrefix(slash, "node_modules/") {
		return true
	}
	if strings.HasSuffix(base, ".pyc") || strings.HasSuffix(base, ".pyo") || base == ".ds_store" {
		return true
	}
	return false
}

func producedResourceRefs(binding execmodel.ExecutionBinding, meta skillmodel.Metadata, produced []string) []string {
	result := make([]string, 0, len(produced))
	base := "work/" + sanitize(binding.ID) + "/skills/" + sanitize(string(meta.PackageID))
	for _, candidate := range produced {
		candidate = normalizeSlash(candidate)
		if !safeRemoteRelativePath(candidate) {
			continue
		}
		if strings.HasPrefix(candidate, "output/") || strings.HasPrefix(candidate, "work/") {
			result = append(result, "run:/"+candidate)
			continue
		}
		result = append(result, "run:/"+base+"/"+candidate)
	}
	return result
}

func sanitize(v string) string {
	v = strings.TrimSpace(v)
	replacer := strings.NewReplacer(`/`, `_`, `\\`, `_`, `:`, `_`, `*`, `_`, `?`, `_`, `"`, `_`, `<`, `_`, `>`, `_`, `|`, `_`)
	return replacer.Replace(v)
}

func normalizeSlash(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.TrimPrefix(value, "./")
	return strings.TrimPrefix(value, "/workspace/")
}

func sandboxLocalDisabled(in execmodel.SandboxProfile) execmodel.SandboxProfile {
	out := in
	out.Mode = execmodel.SandboxDisabled
	out.Provider = ""
	return out
}

func useRemoteSession(sandbox execmodel.SandboxProfile) bool {
	if !strings.EqualFold(strings.TrimSpace(sandbox.Provider), "genesis-sandbox") {
		return false
	}
	return sandbox.Mode == execmodel.SandboxOptional || sandbox.Mode == execmodel.SandboxRequired
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func looksJSON(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")
}

func nodeRuntimeSearchPath(workDir string) string {
	if strings.HasPrefix(strings.TrimSpace(workDir), "/") {
		root := strings.TrimRight(strings.TrimSpace(workDir), "/")
		if root == "" {
			root = "/workspace"
		}
		return joinUniquePaths(":", []string{
			root + "/node_modules",
			root + "/scripts/node_modules",
			"/workspace/node_modules",
			"/opt/genesis-sandbox/image/node_modules",
			"/usr/local/lib/node_modules",
			"/usr/lib/node_modules",
		})
	}
	return nodeModuleSearchPath(workDir)
}

func pythonRuntimeSearchPath(workDir, scriptPath string) string {
	if strings.HasPrefix(strings.TrimSpace(workDir), "/") {
		return scriptPath
	}
	parts := []string{scriptPath}
	return joinUniquePaths(string(os.PathListSeparator), parts)
}

func joinUniquePaths(sep string, values []string) string {
	parts := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		parts = append(parts, value)
	}
	return strings.Join(parts, sep)
}

func nodeModuleSearchPath(workspaceRoot string) string {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	parts := []string{
		filepath.Join(root, "node_modules"),
		filepath.Join(root, "scripts", "node_modules"),
	}
	if depRoot := skillDependencyRoot(root); depRoot != "" {
		parts = append(parts, filepath.Join(depRoot, "node", "node_modules"))
	}
	return joinUniquePaths(string(os.PathListSeparator), parts)
}

func skillDependencyRoot(workDir string) string {
	root := strings.TrimSpace(workDir)
	if root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	skillID := filepath.Base(root)
	if skillID == "" || skillID == "." || skillID == string(filepath.Separator) {
		return ""
	}
	for dir := root; ; {
		if strings.EqualFold(filepath.Base(dir), ".genesis") {
			return filepath.Join(dir, "cache", "skill-deps", skillID)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func venvBinDir(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts")
	}
	return filepath.Join(venvDir, "bin")
}

func venvPythonExists(venvDir string) bool {
	py := filepath.Join(venvBinDir(venvDir), "python")
	if runtime.GOOS == "windows" {
		py = filepath.Join(venvBinDir(venvDir), "python.exe")
	}
	info, err := os.Stat(py)
	return err == nil && !info.IsDir()
}

func shellJoin(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.ContainsAny(part, " \t\"'") {
			out = append(out, `"`+strings.ReplaceAll(part, `"`, `\"`)+`"`)
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, " ")
}

func isExecutableSkillEntry(value string) bool {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\\`, `/`))
	value = strings.TrimPrefix(value, `./`)
	if value == "" {
		return false
	}
	base := filepath.Base(value)
	switch base {
	case "path_contract.py", "__init__.py":
		return false
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		lower := strings.ToLower(strings.TrimSpace(part))
		if lower == "helpers" || lower == "validators" || lower == "schemas" || lower == "__pycache__" {
			return false
		}
	}
	ext := strings.ToLower(filepath.Ext(base))
	return ext == ".py" || ext == ".js" || ext == ".mjs" || ext == ".cjs" || ext == ".sh" || ext == ".ps1" || ext == ".bat" || ext == ".cmd"
}

func isReservedDOSDeviceName(name string) bool {
	name = strings.ToUpper(strings.TrimSpace(name))
	if idx := strings.IndexByte(name, '.'); idx != -1 {
		name = name[:idx]
	}
	switch name {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}
