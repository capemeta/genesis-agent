package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	"genesis-agent/internal/capabilities/skill/script/gate"
	"genesis-agent/internal/capabilities/skill/script/materialize"
	"genesis-agent/internal/capabilities/skill/script/scriptutil"
	scriptworkspace "genesis-agent/internal/capabilities/skill/script/workspace"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/platform/logger/correl"
)

// Service 编排 Skill 脚本 materialize、工作空间与执行。
type Service struct {
	skills                skillcontract.Service
	runner                execcontract.ExecutionRunner
	approval              approvalcontract.Service
	sessionClient         sandboxcontract.SessionClient
	workspaceRef          sandboxcontract.WorkspaceRef
	log                   logger.Logger
	pythonBin             string
	nodeBin               string
	sharedScriptsFS       fs.FS
	sharedForPrefixes     []string
	enablePreflight       bool
	autoRetryAfterInstall bool
	installer             DependencyInstaller
}

// Deps 是 SkillScript Service 依赖。
type Deps struct {
	Skills        skillcontract.Service
	Runner        execcontract.ExecutionRunner
	Approval      approvalcontract.Service
	SessionClient sandboxcontract.SessionClient // 可选；远程/docker 时用于 stage scripts
	WorkspaceRef  sandboxcontract.WorkspaceRef
	Logger        logger.Logger
	PythonBin     string
	NodeBin       string
	// SharedScriptsFS 可选；根为共享包 scripts/（含 office/），合并进 office-* Skill。
	SharedScriptsFS   fs.FS
	SharedForPrefixes []string
	// EnablePreflight 为 true 时在 Run 前探测 dependencies.runtime（默认 false，对齐设计「可选加速」）。
	EnablePreflight bool
	// AutoRetryAfterInstall 为 true 且注入 Installer 时，缺依赖安装后同回合再跑一次（默认 false）。
	AutoRetryAfterInstall bool
	Installer             DependencyInstaller
}

// DependencyInstaller 是可选的同回合安装端口（由产品注入；通常包装 install_skill_dependencies 逻辑）。
type DependencyInstaller interface {
	InstallRuntime(ctx context.Context, skill string, missing []scriptcontract.MissingDep) error
}

// New 创建 SkillScript Service。
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
	log := deps.Logger
	if log == nil {
		log = logger.NewNop()
	}
	python := strings.TrimSpace(deps.PythonBin)
	if python == "" {
		python = "python"
	}
	node := strings.TrimSpace(deps.NodeBin)
	if node == "" {
		node = "node"
	}
	return &Service{
		skills:                deps.Skills,
		runner:                deps.Runner,
		approval:              deps.Approval,
		sessionClient:         deps.SessionClient,
		workspaceRef:          deps.WorkspaceRef,
		log:                   log,
		pythonBin:             python,
		nodeBin:               node,
		sharedScriptsFS:       deps.SharedScriptsFS,
		sharedForPrefixes:     deps.SharedForPrefixes,
		enablePreflight:       deps.EnablePreflight,
		autoRetryAfterInstall: deps.AutoRetryAfterInstall,
		installer:             deps.Installer,
	}, nil
}

// Run 执行 Skill 脚本。
func (s *Service) Run(ctx context.Context, req scriptcontract.RunRequest) (*scriptcontract.RunResult, error) {
	started := time.Now()
	log := correl.AttachLogger(ctx, s.log)
	skillName := strings.TrimSpace(req.Skill)
	scriptID := model.ResourceID(strings.TrimSpace(string(req.Script)))
	if skillName == "" || scriptID == "" {
		return nil, fmt.Errorf("skill与script不能为空")
	}
	if !strings.Contains(string(scriptID), "/scripts/") {
		return nil, fmt.Errorf("script必须是 scripts/ 下的资源ID，例如 office-ppt/scripts/inspect_pptx.py")
	}
	if !scriptutil.IsExecutableScriptEntry(string(scriptID)) {
		out := &scriptcontract.RunResult{
			OK:     false,
			Skill:  skillName,
			Script: string(scriptID),
			Error:  "禁止将辅助模块作为入口执行（如 path_contract.py）；请使用 inspect_*.py / render_*.py 等业务脚本。path_contract 仅供其它脚本 import",
		}
		classifyFailure(out)
		return out, nil
	}

	meta, err := s.skills.Resolve(ctx, skillcontract.ResolveRequest{CatalogRequest: req.Catalog, Name: skillName})
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		if id, ok := contextutil.GetRunID(ctx); ok {
			runID = id
		}
	}
	ws, err := scriptworkspace.PrepareLocalTask(req.WorkspaceRoot, runID)
	if err != nil {
		return nil, err
	}
	skillDir := filepath.Join(ws.WorkDir, "skills", string(meta.PackageID))
	mat := &materialize.Materializer{
		Service:           s.skills,
		SharedScriptsFS:   s.sharedScriptsFS,
		SharedForPrefixes: s.sharedForPrefixes,
	}
	matResult, err := mat.MaterializePackageScripts(ctx, req.Catalog, meta, skillDir)
	if err != nil {
		return nil, err
	}
	ws.SkillDir = matResult.SkillDir

	staged, err := stageInputs(req.WorkspaceRoot, ws.InputDir, req.Inputs)
	if err != nil {
		return nil, err
	}

	relScript := strings.TrimPrefix(string(scriptID), string(meta.PackageID)+"/")
	scriptPath := filepath.Join(matResult.SkillDir, filepath.FromSlash(relScript))
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, fmt.Errorf("脚本未 materialize: %s", scriptID)
	}
	// 相对 scripts/ 的路径（保留 office/ 等子目录），保证嵌套脚本的 sys.path[0] 正确。
	scriptRel, err := filepath.Rel(matResult.ScriptsDir, scriptPath)
	if err != nil {
		return nil, fmt.Errorf("解析脚本相对路径失败: %w", err)
	}
	scriptRel = filepath.ToSlash(scriptRel)
	if scriptRel == ".." || strings.HasPrefix(scriptRel, "../") {
		return nil, fmt.Errorf("脚本路径越界: %s", scriptID)
	}

	decision, err := s.approval.Authorize(ctx, approvalmodel.Request{
		ToolName: "run_skill_script",
		Action:   approvalmodel.ActionCommandExec,
		Resource: approvalmodel.Resource{Type: "skill_script", URI: string(scriptID), Display: string(scriptID)},
		Reason:   "执行 Skill 脚本",
		Risk:     approvalmodel.RiskMedium,
		SuggestedScopes: []approvalmodel.GrantScope{
			approvalmodel.GrantScopeOnce,
			approvalmodel.GrantScopeSession,
		},
		Metadata: map[string]string{
			"skill":        meta.Name,
			"script":       string(scriptID),
			"skill_script": "true",
		},
	})
	if err != nil {
		return nil, err
	}
	if decision.Type != approvalmodel.DecisionApproved && decision.Type != approvalmodel.DecisionApprovedForScope {
		out := &scriptcontract.RunResult{
			OK: false, Skill: meta.Name, Script: string(scriptID), Error: fmt.Sprintf("approval %s: %s", decision.Type, decision.Reason),
		}
		classifyFailure(out)
		return out, nil
	}

	// Mode/Provider/WorkspaceID 来自产品 sandbox 配置；workload profile 由 Skill 推断，
	// 不能被 CLI 默认的 code-polyglot-basic 覆盖。
	sandbox := req.Sandbox
	sandbox.TaskType = inferTaskType(meta)
	sandbox.Operation = execmodel.SandboxOperationRunSkill
	sandbox.RuntimeProfile = inferProfile(meta, sandbox.TaskType)
	if sandbox.Metadata == nil {
		sandbox.Metadata = map[string]string{}
	}
	sandbox.Metadata["source"] = "skill"
	sandbox.Metadata["skill_id"] = meta.Name
	sandbox.Metadata["skill_package"] = string(meta.PackageID)

	timeout := 120 * time.Second
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS) * time.Millisecond
	}
	runtimeBin := s.runtimeBin(scriptRel, req.PythonBin)
	args := append([]string{scriptRel}, req.Args...)
	cmdLine := shellJoin(append([]string{runtimeBin}, args...))

	out := &scriptcontract.RunResult{
		Skill:        meta.Name,
		Script:       string(scriptID),
		Command:      cmdLine,
		SkillDir:     ws.SkillDir,
		InputDir:     ws.InputDir,
		OutputDir:    ws.OutputDir,
		StagedInputs: staged,
		DurationMS:   time.Since(started).Milliseconds(),
		Metadata: map[string]string{
			"runtime_profile": string(sandbox.RuntimeProfile),
			"task_type":       string(sandbox.TaskType),
			"materialized":    fmt.Sprintf("%d", len(matResult.Files)),
			"runtime_bin":     runtimeBin,
		},
	}

	log.Info("执行Skill脚本", "skill", meta.Name, "script", scriptID, "profile", sandbox.RuntimeProfile, "mode", string(sandbox.Mode))

	// Preflight：声明了 runtime 时先探测，失败直接返回同一 failure_kind（设计 §6.9）。
	// system 缺失：warning 后继续（镜像边界）；npm/pip 缺失：硬失败或 opt-in 安装后重试。
	if s.enablePreflight {
		if miss := s.preflightRuntime(ctx, meta, scriptRel, req.WorkspaceRoot); len(miss) > 0 {
			installable := installableMissing(miss)
			if len(installable) == 0 {
				for _, m := range miss {
					if m.Manager == "system" {
						cmd := m.Require
						if cmd == "" {
							cmd = m.Name
						}
						out.Warnings = append(out.Warnings, "preflight: system binary missing on PATH (use image/local toolchain): "+cmd)
					}
				}
			} else {
				out.OK = false
				out.FailureKind = "dependency_missing"
				out.Missing = miss // 保留 system 条目供模型看见，但 suggested_install 只含可装包
				out.Error = "preflight: dependency_missing"
				out.SuggestedAction = "install_then_retry"
				out.Retryable = true
				out.SuggestedInstall = buildSuggestedInstall(meta.Name, miss)
				out.Metadata["backend"] = "preflight"
				classifyFailure(out)
				if !s.tryAutoRetryInstall(ctx, meta.Name, out) {
					out.DurationMS = time.Since(started).Milliseconds()
					return out, nil
				}
				// 安装成功：继续下方执行路径（最多再跑一次业务脚本）。
			}
		}
	}

	for attempt := 0; attempt < 2; attempt++ {
		var result *execmodel.Result
		if useRemoteSession(sandbox) {
			result, err = s.runRemoteOrDegrade(ctx, meta, matResult, staged, ws, sandbox, runtimeBin, scriptRel, req.Args, timeout, cmdLine, req.WorkspaceRoot, out)
		} else {
			result, err = s.runLocal(ctx, cmdLine, matResult.ScriptsDir, scriptRel, req.WorkspaceRoot, sandbox, ws, timeout)
			if out.Metadata["backend"] == "" || out.Metadata["backend"] == "preflight" {
				out.Metadata["backend"] = "local"
			}
		}
		if err != nil {
			out.OK = false
			out.Error = err.Error()
			classifyFailure(out)
			if attempt == 0 && s.tryAutoRetryInstall(ctx, meta.Name, out) {
				continue
			}
			out.DurationMS = time.Since(started).Milliseconds()
			return out, nil
		}
		if result == nil {
			out.OK = false
			out.Error = "execution runner返回空结果"
			classifyFailure(out)
			out.DurationMS = time.Since(started).Milliseconds()
			return out, nil
		}
		out.ExitCode = result.ExitCode
		out.Stdout = result.Stdout
		out.Stderr = result.Stderr
		out.OK = false
		out.FailureKind = ""
		if result.TimedOut {
			out.FailureKind = "timeout"
			if out.Error == "" {
				out.Error = "script timed out"
			}
		}
		if len(result.SandboxViolations) > 0 {
			out.FailureKind = "sandbox_violation"
			out.Warnings = append(out.Warnings, "sandbox_violations: "+strings.Join(result.SandboxViolations, "; "))
			if out.Error == "" {
				out.Error = "sandbox violation"
			}
		}
		if result.ExitCode != 0 {
			out.Error = fmt.Sprintf("script exit_code=%d", result.ExitCode)
			if result.Error != "" {
				out.Error = result.Error
			}
		} else if !result.TimedOut && len(result.SandboxViolations) == 0 {
			out.OK = true
			out.Error = ""
		}
		// 空输出且无 stderr：多为辅助模块被当入口执行（import 后直接退出）。
		if out.OK && strings.TrimSpace(result.Stdout) == "" && strings.TrimSpace(result.Stderr) == "" {
			out.OK = false
			out.Error = "脚本无 stdout/stderr 输出；可能误执行了辅助模块，或脚本未按契约打印结果。请改用 list_skill_resources 确认的可执行脚本"
		}
		artifacts, warnings := collectArtifacts(ws.OutputDir)
		// 远程产物也可能在 Result.Artifacts
		for _, a := range result.Artifacts {
			if a.LocalPath == "" {
				continue
			}
			ok, kind, reason := gate.CheckDelivery(a.LocalPath)
			artifacts = append(artifacts, scriptcontract.Artifact{
				Name: firstNonEmpty(a.Name, filepath.Base(a.LocalPath)), Path: a.LocalPath, Size: a.Size, Kind: kind, OK: ok, Reason: reason,
			})
			if !ok {
				warnings = append(warnings, a.Name+": "+reason)
			}
		}
		out.Artifacts = artifacts
		out.Warnings = append(out.Warnings, warnings...)
		if out.OK && looksJSON(result.Stdout) {
			var payload map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &payload) == nil {
				if ok, exists := payload["ok"].(bool); exists && !ok {
					out.OK = false
					out.Error = "script reported ok=false"
				}
			}
		}
		// 交付物门禁失败则整体失败（有 .pptx/.docx/.xlsx/.pdf 时）。
		for _, art := range out.Artifacts {
			ext := strings.ToLower(filepath.Ext(art.Name))
			if (ext == ".pptx" || ext == ".docx" || ext == ".xlsx" || ext == ".pdf") && !art.OK {
				out.OK = false
				if out.Error == "" {
					out.Error = "artifact gate failed: " + art.Reason
				}
			}
		}
		classifyFailure(out)
		// 执行期缺依赖：opt-in 时可装包后同回合再跑一次（设计 §6.7；默认关）。
		if !out.OK && attempt == 0 && s.tryAutoRetryInstall(ctx, meta.Name, out) {
			continue
		}
		out.DurationMS = time.Since(started).Milliseconds()
		return out, nil
	}
	out.DurationMS = time.Since(started).Milliseconds()
	return out, nil
}

func (s *Service) runRemote(ctx context.Context, meta model.Metadata, mat *materialize.Result, staged []string, localWS execmodel.ExecutionWorkspace, sandbox execmodel.SandboxProfile, runtimeBin, scriptRel string, args []string, timeout time.Duration) (*execmodel.Result, error) {
	session, err := s.sessionClient.OpenSession(ctx, sandboxcontract.SessionOptions{
		Workspace: s.workspaceRef,
		Sandbox:   sandbox,
		Options:   execcontract.RunOptions{Timeout: timeout, Sandbox: sandbox},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close(context.Background()) }()

	inputArtifacts := make([]execmodel.InputArtifactRef, 0)
	// stage scripts under skills/<pkg>/scripts/
	err = filepath.Walk(mat.ScriptsDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(mat.ScriptsDir, p)
		if err != nil {
			return err
		}
		name := path.Join("skills", string(meta.PackageID), "scripts", filepath.ToSlash(rel))
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		stagedArt, err := session.StageInput(ctx, sandboxcontract.StageInputRequest{Name: name, Content: bytes.NewReader(data)})
		if err != nil {
			return err
		}
		inputArtifacts = append(inputArtifacts, stagedArt.Artifact)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, name := range staged {
		data, err := os.ReadFile(filepath.Join(localWS.InputDir, name))
		if err != nil {
			return nil, err
		}
		stagedArt, err := session.StageInput(ctx, sandboxcontract.StageInputRequest{Name: name, Content: bytes.NewReader(data)})
		if err != nil {
			return nil, err
		}
		inputArtifacts = append(inputArtifacts, stagedArt.Artifact)
	}

	remoteSkillDir := "/workspace/input/skills/" + string(meta.PackageID)
	remoteScripts := remoteSkillDir + "/scripts"
	ws := execmodel.ExecutionWorkspace{
		Mode:       execmodel.WorkspaceModeSandboxSess,
		PathPolicy: execmodel.PathPolicyStrictWorkspace,
		WorkDir:    "/workspace",
		InputDir:   "/workspace/input",
		OutputDir:  "/workspace/output",
		TmpDir:     "/workspace/tmp",
		SkillDir:   remoteSkillDir,
	}
	cmdLine := shellJoin(append([]string{runtimeBin, scriptRel}, args...))
	return session.Run(ctx, sandboxcontract.CommandRequest{
		Workspace: s.workspaceRef,
		Command: execmodel.Command{
			Command: cmdLine,
			Cwd:     remoteScripts,
			Shell:   "auto",
			Env:     map[string]string{"PYTHONPATH": remoteScripts},
		},
		Sandbox: sandbox,
		Options: execcontract.RunOptions{
			Timeout:        timeout,
			Sandbox:        sandbox,
			Workspace:      ws,
			InputArtifacts: inputArtifacts,
		},
	})
}

func useRemoteSession(sandbox execmodel.SandboxProfile) bool {
	// SandboxProfile.Mode 是 disabled/optional/required；远程判定看 Provider。
	if !strings.EqualFold(strings.TrimSpace(sandbox.Provider), "genesis-sandbox") {
		return false
	}
	return sandbox.Mode == execmodel.SandboxOptional || sandbox.Mode == execmodel.SandboxRequired
}

// runRemoteOrDegrade 对齐 I6 / execution.Runner：optional 可降级本地；required fail closed。
func (s *Service) runRemoteOrDegrade(
	ctx context.Context,
	meta model.Metadata,
	mat *materialize.Result,
	staged []string,
	ws execmodel.ExecutionWorkspace,
	sandbox execmodel.SandboxProfile,
	runtimeBin, scriptRel string,
	args []string,
	timeout time.Duration,
	cmdLine string,
	workspaceRoot string,
	out *scriptcontract.RunResult,
) (*execmodel.Result, error) {
	log := correl.AttachLogger(ctx, s.log)
	if s.sessionClient == nil {
		if sandbox.Mode == execmodel.SandboxRequired {
			return nil, fmt.Errorf("genesis-sandbox SessionClient未配置，且 sandbox.mode=required，拒绝降级")
		}
		// optional：降级本地，必须留下 warning（与 run_command 一致）。
		warn := "skill_script_sandbox_fallback: SessionClient未配置，已降级本地执行"
		log.Warn(warn, "skill", meta.Name, "mode", string(sandbox.Mode))
		out.Warnings = append(out.Warnings, warn)
		out.Metadata["backend"] = "local_degraded"
		out.Metadata["sandbox_degraded"] = "true"
		return s.runLocal(ctx, cmdLine, mat.ScriptsDir, scriptRel, workspaceRoot, sandboxLocalDisabled(sandbox), ws, timeout)
	}
	result, err := s.runRemote(ctx, meta, mat, staged, ws, sandbox, runtimeBin, scriptRel, args, timeout)
	if err != nil && sandbox.Mode == execmodel.SandboxOptional && execcontract.CodeOf(err) == execcontract.ErrCodeSandboxUnavailable {
		warn := "skill_script_sandbox_fallback: " + err.Error()
		log.Warn("远程沙箱不可用，降级本地执行", "error", err, "skill", meta.Name)
		out.Warnings = append(out.Warnings, warn)
		out.Metadata["backend"] = "local_degraded"
		out.Metadata["sandbox_degraded"] = "true"
		return s.runLocal(ctx, cmdLine, mat.ScriptsDir, scriptRel, workspaceRoot, sandboxLocalDisabled(sandbox), ws, timeout)
	}
	if err == nil {
		out.Metadata["backend"] = "remote_session"
	}
	return result, err
}

func sandboxLocalDisabled(in execmodel.SandboxProfile) execmodel.SandboxProfile {
	out := in
	out.Mode = execmodel.SandboxDisabled
	out.Provider = ""
	return out
}

func (s *Service) runLocal(ctx context.Context, cmdLine, scriptsDir, scriptRel, workspaceRoot string, sandbox execmodel.SandboxProfile, ws execmodel.ExecutionWorkspace, timeout time.Duration) (*execmodel.Result, error) {
	cmd := execmodel.Command{
		Command: cmdLine,
		Cwd:     scriptsDir,
		Shell:   "auto",
	}
	if strings.EqualFold(filepath.Ext(scriptRel), ".js") || strings.EqualFold(filepath.Ext(scriptRel), ".mjs") || strings.EqualFold(filepath.Ext(scriptRel), ".cjs") {
		cmd.Env = map[string]string{"NODE_PATH": nodeModuleSearchPath(workspaceRoot)}
	}
	return s.runner.Run(ctx, cmd, execcontract.RunOptions{
		Timeout:   timeout,
		Sandbox:   sandbox,
		Workspace: ws,
	})
}

func stageInputs(workspaceRoot, inputDir string, inputs []string) ([]string, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		root = wd
	}
	staged := make([]string, 0, len(inputs))
	for _, raw := range inputs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		src := raw
		if !filepath.IsAbs(src) {
			src = filepath.Join(root, raw)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, fmt.Errorf("读取输入失败 %s: %w", raw, err)
		}
		name := filepath.Base(src)
		dest := filepath.Join(inputDir, name)
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return nil, err
		}
		staged = append(staged, name)
	}
	return staged, nil
}

func collectArtifacts(outputDir string) ([]scriptcontract.Artifact, []string) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil, []string{err.Error()}
	}
	out := make([]scriptcontract.Artifact, 0)
	warnings := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(outputDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		ok, kind, reason := gate.CheckDelivery(path)
		art := scriptcontract.Artifact{Name: entry.Name(), Path: path, Size: info.Size(), Kind: kind, OK: ok, Reason: reason}
		out = append(out, art)
		if !ok {
			warnings = append(warnings, entry.Name()+": "+reason)
		}
	}
	return out, warnings
}

func inferTaskType(meta model.Metadata) execmodel.SandboxTaskType {
	name := strings.ToLower(meta.Name)
	switch {
	case strings.Contains(name, "office"), strings.Contains(name, "ppt"), strings.Contains(name, "word"), strings.Contains(name, "excel"), strings.Contains(name, "pdf"):
		return execmodel.SandboxTaskOffice
	default:
		return execmodel.SandboxTaskSkill
	}
}

func inferProfile(meta model.Metadata, taskType execmodel.SandboxTaskType) execmodel.SandboxRuntimeProfile {
	if taskType == execmodel.SandboxTaskOffice {
		return execmodel.RuntimeProfileOfficeBasic
	}
	return execmodel.RuntimeProfileSkillPolyglotBasic
}

func (s *Service) runtimeBin(scriptRel, pythonOverride string) string {
	ext := strings.ToLower(filepath.Ext(scriptRel))
	switch ext {
	case ".js", ".mjs", ".cjs":
		return firstNonEmpty(s.nodeBin, "node")
	default:
		return firstNonEmpty(pythonOverride, s.pythonBin, "python")
	}
}

func nodeModuleSearchPath(workspaceRoot string) string {
	parts := make([]string, 0, 3)
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}
	if root != "" {
		parts = append(parts, filepath.Join(root, "node_modules"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		parts = append(parts, filepath.Join(home, ".node_modules"))
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func shellJoin(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.ContainsAny(p, " \t\"'") {
			out = append(out, `"`+strings.ReplaceAll(p, `"`, `\"`)+`"`)
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, " ")
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
