package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	sandboxsession "genesis-agent/internal/capabilities/sandbox/session"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	"genesis-agent/internal/capabilities/skill/script/gate"
	"genesis-agent/internal/capabilities/skill/script/materialize"
	scriptworkspace "genesis-agent/internal/capabilities/skill/script/workspace"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger"
)

const sessionIdleTTL = 10 * time.Minute

// Service 编排 Skill 命令 materialize、工作空间与执行。
type Service struct {
	skills                skillcontract.Service
	runner                execcontract.ExecutionRunner
	approval              approvalcontract.Service
	sessionClient         sandboxcontract.SessionClient
	fileClient            sandboxcontract.FileSystemClient
	workspaceRef          sandboxcontract.WorkspaceRef
	log                   logger.Logger
	pythonBin             string
	nodeBin               string
	enablePreflight       bool
	autoRetryAfterInstall bool
	installer             DependencyInstaller

	mu       sync.Mutex
	sessions map[string]*remoteSession
}

type remoteSession struct {
	session      *sandboxsession.Session
	skillDir     string
	staged       map[string]struct{}
	materialized bool
	lastUsed     time.Time
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
}

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
	log := deps.Logger
	if log == nil {
		log = logger.NewNop()
	}
	return &Service{
		skills:                deps.Skills,
		runner:                deps.Runner,
		approval:              deps.Approval,
		sessionClient:         deps.SessionClient,
		fileClient:            deps.FileClient,
		workspaceRef:          deps.WorkspaceRef,
		log:                   log,
		pythonBin:             "python",
		nodeBin:               "node",
		enablePreflight:       deps.EnablePreflight,
		autoRetryAfterInstall: deps.AutoRetryAfterInstall,
		installer:             deps.Installer,
		sessions:              make(map[string]*remoteSession),
	}, nil
}

// Close 释放 Service 缓存的远端 sandbox session。
func (s *Service) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	sessions := make([]*sandboxsession.Session, 0, len(s.sessions))
	for key, item := range s.sessions {
		if item != nil && item.session != nil {
			sessions = append(sessions, item.session)
		}
		delete(s.sessions, key)
	}
	s.mu.Unlock()
	var firstErr error
	for _, sess := range sessions {
		if err := sess.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) Run(ctx context.Context, req scriptcontract.RunRequest) (*scriptcontract.RunResult, error) {
	started := time.Now()
	skillName := strings.TrimSpace(req.Skill)
	command := strings.TrimSpace(req.Command)
	if skillName == "" || command == "" {
		return nil, fmt.Errorf("skill与command不能为空")
	}
	req.Command = command
	scriptHint := commandScriptHint(command)
	if scriptHint != "" && !isExecutableSkillEntry(scriptHint) {
		out := &scriptcontract.RunResult{OK: false, Skill: skillName, Script: scriptHint, Command: command, Error: "禁止将辅助模块作为入口执行（如 path_contract.py）；请改用业务脚本入口"}
		classifyFailure(out)
		return out, nil
	}
	meta, err := s.skills.Resolve(ctx, skillcontract.ResolveRequest{CatalogRequest: req.Catalog, Name: skillName})
	if err != nil {
		return nil, err
	}
	sandbox := req.Sandbox
	sandbox.TaskType = resolveTaskType(meta)
	sandbox.Operation = execmodel.SandboxOperationRunSkill
	sandbox.RuntimeProfile = resolveRuntimeProfile(meta, sandbox.TaskType)
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
		classifyFailureForSkill(out, meta.Dependencies)
		return out, nil
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		if id, ok := contextutil.GetRunID(ctx); ok {
			runID = id
		}
	}
	if runID == "" {
		runID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	}

	decision, err := s.approval.Authorize(ctx, approvalmodel.Request{
		ToolName:        "run_skill_command",
		Action:          approvalmodel.ActionCommandExec,
		Resource:        approvalmodel.Resource{Type: "skill_command", URI: meta.Name, Display: meta.Name},
		Reason:          "执行 Skill 命令",
		Risk:            approvalmodel.RiskMedium,
		SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce, approvalmodel.GrantScopeSession},
		Metadata:        map[string]string{"skill": meta.Name, "command": command},
	})
	if err != nil {
		return nil, err
	}
	if decision.Type != approvalmodel.DecisionApproved && decision.Type != approvalmodel.DecisionApprovedForScope {
		out := &scriptcontract.RunResult{OK: false, Skill: meta.Name, Command: command, Error: fmt.Sprintf("approval %s: %s", decision.Type, decision.Reason)}
		classifyFailure(out)
		return out, nil
	}

	timeout := 120 * time.Second
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS) * time.Millisecond
	}
	out := &scriptcontract.RunResult{
		OK:      false,
		Skill:   meta.Name,
		Script:  commandScriptHint(command),
		Command: command,
		Metadata: map[string]string{
			"runtime_profile": string(sandbox.RuntimeProfile),
			"task_type":       string(sandbox.TaskType),
		},
	}

	if s.enablePreflight && !useRemoteSession(sandbox) {
		if miss := s.preflightRuntime(ctx, meta, out.Script, req.WorkspaceRoot); len(miss) > 0 {
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
		var result *execmodel.Result
		var produced []string
		var workDir string
		var staged []string
		var artifacts []scriptcontract.Artifact
		var warnings []string
		if useRemoteSession(sandbox) {
			result, produced, workDir, staged, artifacts, warnings, err = s.runRemote(ctx, meta, runID, req, timeout, sandbox)
			if out.Metadata["backend"] == "" {
				out.Metadata["backend"] = "remote_session"
			}
		} else {
			result, produced, workDir, staged, err = s.runLocal(ctx, meta, runID, req, timeout, sandbox)
			if out.Metadata["backend"] == "" {
				out.Metadata["backend"] = "local"
			}
			artifacts, warnings = collectArtifactsByProduced(workDir, produced)
		}
		out.WorkDir = workDir
		out.SkillDir = workDir
		out.Produced = append([]string(nil), produced...)
		out.StagedInputs = append([]string(nil), staged...)
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
		out.Artifacts = artifacts
		out.Warnings = append(out.Warnings, warnings...)
		for _, art := range out.Artifacts {
			ext := strings.ToLower(filepath.Ext(art.Name))
			if (ext == ".pptx" || ext == ".docx" || ext == ".xlsx" || ext == ".pdf") && !art.OK {
				out.OK = false
				if out.Error == "" {
					out.Error = "artifact gate failed: " + art.Reason
				}
			}
		}
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

func (s *Service) runLocal(ctx context.Context, meta skillmodel.Metadata, runID string, req scriptcontract.RunRequest, timeout time.Duration, sandbox execmodel.SandboxProfile) (*execmodel.Result, []string, string, []string, error) {
	ws, err := scriptworkspace.PrepareLocalTask(req.WorkspaceRoot, runID)
	if err != nil {
		return nil, nil, "", nil, err
	}
	skillDir := filepath.Join(ws.WorkDir, "skills", sanitize(string(meta.PackageID)))
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return nil, nil, "", nil, err
	}
	mat := &materialize.Materializer{Service: s.skills}
	if _, err := mat.MaterializePackageScripts(ctx, req.Catalog, meta, skillDir); err != nil {
		return nil, nil, "", nil, err
	}
	staged, err := stageInputs(req.WorkspaceRoot, ws, skillDir, req.Inputs)
	if err != nil {
		return nil, nil, skillDir, nil, err
	}
	before, err := snapshotLocalFiles(skillDir)
	if err != nil {
		return nil, nil, skillDir, staged, err
	}
	cmd := execmodel.Command{Command: req.Command, Cwd: skillDir, Shell: "auto", Env: skillEnv(skillDir, ws.TmpDir)}
	result, err := s.runner.Run(ctx, cmd, execcontract.RunOptions{Timeout: timeout, Sandbox: sandbox, Workspace: buildSkillWorkspace(ws, skillDir)})
	if err != nil {
		return nil, nil, skillDir, staged, err
	}
	after, err := snapshotLocalFiles(skillDir)
	if err != nil {
		return nil, nil, skillDir, staged, err
	}
	produced := diffSnapshots(before, after)
	return result, produced, skillDir, staged, nil
}

func (s *Service) runRemote(ctx context.Context, meta skillmodel.Metadata, runID string, req scriptcontract.RunRequest, timeout time.Duration, sandbox execmodel.SandboxProfile) (*execmodel.Result, []string, string, []string, []scriptcontract.Artifact, []string, error) {
	if s.sessionClient == nil || s.fileClient == nil {
		if sandbox.Mode == execmodel.SandboxRequired {
			return nil, nil, "", nil, nil, nil, fmt.Errorf("genesis-sandbox session/file client未配置，且 sandbox.mode=required，拒绝降级")
		}
		result, produced, workDir, staged, err := s.runLocal(ctx, meta, runID, req, timeout, sandboxLocalDisabled(sandbox))
		artifacts, warnings := collectArtifactsByProduced(workDir, produced)
		warnings = append([]string{"genesis-sandbox session/file client未配置，sandbox optional 已降级到本地执行"}, warnings...)
		return result, produced, workDir, staged, artifacts, warnings, err
	}
	s.cleanupStaleSessions(context.Background())
	key := sessionKey(runID, meta.Name)
	s.mu.Lock()
	cached := s.sessions[key]
	s.mu.Unlock()
	if cached == nil {
		sess, err := sandboxsession.Open(ctx, sandboxsession.Deps{Sessions: s.sessionClient, Files: s.fileClient}, sandboxsession.Options{Workspace: s.workspaceRef, Sandbox: sandbox})
		if err != nil {
			openErr := err
			if sandbox.Mode == execmodel.SandboxRequired {
				return nil, nil, "", nil, nil, nil, err
			}
			result, produced, workDir, staged, err := s.runLocal(ctx, meta, runID, req, timeout, sandboxLocalDisabled(sandbox))
			artifacts, warnings := collectArtifactsByProduced(workDir, produced)
			warnings = append([]string{"genesis-sandbox session打开失败，sandbox optional 已降级到本地执行: " + openErr.Error()}, warnings...)
			return result, produced, workDir, staged, artifacts, warnings, err
		}
		cached = &remoteSession{session: sess, skillDir: "/workspace", staged: map[string]struct{}{}, lastUsed: time.Now()}
		s.mu.Lock()
		s.sessions[key] = cached
		s.mu.Unlock()
	}
	cached.lastUsed = time.Now()
	if !cached.materialized {
		mat := &materialize.Materializer{Service: s.skills}
		localWS, err := scriptworkspace.PrepareLocalTask(req.WorkspaceRoot, runID+"-materialize")
		if err != nil {
			return nil, nil, cached.skillDir, nil, nil, nil, err
		}
		localSkillDir := filepath.Join(localWS.WorkDir, "skills", sanitize(string(meta.PackageID)))
		matResult, err := mat.MaterializePackageScripts(ctx, req.Catalog, meta, localSkillDir)
		if err != nil {
			return nil, nil, cached.skillDir, nil, nil, nil, err
		}
		for _, rel := range matResult.PackageFiles {
			data, err := os.ReadFile(filepath.Join(matResult.SkillDir, filepath.FromSlash(rel)))
			if err != nil {
				return nil, nil, cached.skillDir, nil, nil, nil, err
			}
			if err := cached.session.WriteFile(ctx, rel, data, fscontract.WriteOptions{CreateParents: true, Overwrite: true}); err != nil {
				return nil, nil, cached.skillDir, nil, nil, nil, err
			}
		}
		cached.materialized = true
	}
	ws, err := scriptworkspace.PrepareLocalTask(req.WorkspaceRoot, runID)
	if err != nil {
		return nil, nil, cached.skillDir, nil, nil, nil, err
	}
	localStageDir := filepath.Join(ws.WorkDir, "skills", sanitize(string(meta.PackageID)))
	if err := os.MkdirAll(localStageDir, 0o755); err != nil {
		return nil, nil, cached.skillDir, nil, nil, nil, err
	}
	staged, err := stageInputs(req.WorkspaceRoot, ws, localStageDir, req.Inputs)
	if err != nil {
		return nil, nil, cached.skillDir, nil, nil, nil, err
	}
	for _, name := range staged {
		if _, ok := cached.staged[name]; ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(localStageDir, name))
		if err != nil {
			return nil, nil, cached.skillDir, staged, nil, nil, err
		}
		if err := cached.session.WriteFile(ctx, name, data, fscontract.WriteOptions{}); err != nil {
			return nil, nil, cached.skillDir, staged, nil, nil, err
		}
		cached.staged[name] = struct{}{}
	}
	before, err := snapshotRemoteFiles(ctx, cached.session)
	if err != nil {
		return nil, nil, cached.skillDir, staged, nil, nil, err
	}
	result, err := cached.session.Run(ctx, execmodel.Command{Command: req.Command, Cwd: "/workspace", Shell: "auto", Env: skillEnv("/workspace", "/workspace/tmp")}, execcontract.RunOptions{Timeout: timeout, Sandbox: sandbox, Workspace: remoteSkillWorkspace()})
	if err != nil {
		return nil, nil, cached.skillDir, staged, nil, nil, err
	}
	after, err := snapshotRemoteFiles(ctx, cached.session)
	if err != nil {
		return nil, nil, cached.skillDir, staged, nil, nil, err
	}
	produced := diffSnapshots(before, after)
	artifacts, warnings := collectRemoteArtifactsByProduced(ctx, cached.session, filepath.Join(ws.WorkDir, "artifacts", sanitize(string(meta.PackageID))), produced)
	return result, produced, cached.skillDir, staged, artifacts, warnings, nil
}

func (s *Service) cleanupStaleSessions(ctx context.Context) {
	now := time.Now()
	var stale []*sandboxsession.Session
	s.mu.Lock()
	for key, item := range s.sessions {
		if item == nil || item.session == nil || now.Sub(item.lastUsed) <= sessionIdleTTL {
			continue
		}
		stale = append(stale, item.session)
		delete(s.sessions, key)
	}
	s.mu.Unlock()
	for _, sess := range stale {
		_ = sess.Close(ctx)
	}
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
	env["PYTHONPATH"] = pyPath
	env["NODE_PATH"] = nodeRuntimeSearchPath(workDir)
	return env
}

func buildSkillWorkspace(base execmodel.ExecutionWorkspace, skillDir string) execmodel.ExecutionWorkspace {
	base.Mode = execmodel.WorkspaceModeLocalTask
	base.WorkDir = skillDir
	base.InputDir = skillDir
	base.OutputDir = skillDir
	base.SkillDir = skillDir
	return base
}

func remoteSkillWorkspace() execmodel.ExecutionWorkspace {
	return execmodel.ExecutionWorkspace{Mode: execmodel.WorkspaceModeSandboxSess, PathPolicy: execmodel.PathPolicyStrictWorkspace, WorkDir: "/workspace", InputDir: "/workspace", OutputDir: "/workspace", TmpDir: "/workspace/tmp", SkillDir: "/workspace"}
}

func resolveTaskType(meta skillmodel.Metadata) execmodel.SandboxTaskType {
	if isOfficeRuntime(meta.Dependencies.Runtime) {
		return execmodel.SandboxTaskOffice
	}
	return execmodel.SandboxTaskSkill
}

func resolveRuntimeProfile(meta skillmodel.Metadata, taskType execmodel.SandboxTaskType) execmodel.SandboxRuntimeProfile {
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
	for _, part := range strings.Fields(strings.ReplaceAll(command, "\"", "")) {
		clean := strings.TrimSpace(strings.ReplaceAll(part, "\\", "/"))
		if strings.HasPrefix(clean, "scripts/") {
			return clean
		}
	}
	return ""
}

func sessionKey(runID, skill string) string {
	return runID + "::" + strings.ToLower(strings.TrimSpace(skill))
}

func stageInputs(workspaceRoot string, ws execmodel.ExecutionWorkspace, destDir string, inputs []string) ([]string, error) {
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
	rootReal, err := boundaryPath(root)
	if err != nil {
		return nil, fmt.Errorf("解析工作区失败: %w", err)
	}
	staged := make([]string, 0, len(inputs))
	for _, raw := range inputs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		srcReal, tried, err := resolveStageSource(raw, rootReal, ws)
		if err != nil {
			return nil, fmt.Errorf("解析输入失败 %s: %w（已尝试: %s）", raw, err, strings.Join(tried, ", "))
		}
		if !isWithinPath(srcReal, rootReal) {
			return nil, fmt.Errorf("输入路径必须位于工作区内: %s", raw)
		}
		data, err := os.ReadFile(srcReal)
		if err != nil {
			return nil, fmt.Errorf("读取输入失败 %s: %w", raw, err)
		}
		name := filepath.Base(srcReal)
		dest := filepath.Join(destDir, name)
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return nil, err
		}
		staged = append(staged, name)
	}
	return staged, nil
}

func resolveStageSource(raw, workspaceRoot string, ws execmodel.ExecutionWorkspace) (string, []string, error) {
	tried := make([]string, 0, 8)
	tryFile := func(candidate string) (string, bool) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return "", false
		}
		real, err := boundaryPath(candidate)
		if err != nil {
			tried = append(tried, candidate)
			return "", false
		}
		tried = append(tried, real)
		info, err := os.Stat(real)
		if err != nil || info.IsDir() {
			return "", false
		}
		return real, true
	}
	if rel, ok := scriptworkspace.StripLogicalDirPrefix(raw); ok {
		base := scriptworkspace.DirBase(rel.Prefix, ws)
		if base == "" {
			return "", tried, fmt.Errorf("逻辑目录 %s 未注入", rel.Prefix)
		}
		if found, ok := tryFile(filepath.Join(base, filepath.FromSlash(rel.Rest))); ok {
			return found, tried, nil
		}
		return "", tried, fmt.Errorf("文件不存在")
	}
	if filepath.IsAbs(raw) {
		if found, ok := tryFile(raw); ok {
			return found, tried, nil
		}
		return "", tried, fmt.Errorf("文件不存在")
	}
	candidates := []string{filepath.Join(workspaceRoot, raw), filepath.Join(ws.WorkDir, raw), filepath.Join(ws.OutputDir, raw), filepath.Join(ws.InputDir, raw), filepath.Join(ws.TmpDir, raw)}
	for _, c := range candidates {
		if found, ok := tryFile(c); ok {
			return found, tried, nil
		}
	}
	return "", tried, fmt.Errorf("文件不存在")
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

func snapshotRemoteFiles(ctx context.Context, sess *sandboxsession.Session) (map[string]fileFingerprint, error) {
	walk, err := sess.Walk(ctx, ".", fscontract.WalkOptions{})
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
		out[normalizeSlash(entry.Path)] = fileFingerprint{Size: entry.Size, ModTime: entry.ModifiedAt.UnixNano()}
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
		prev, ok := before[path]
		if !ok || prev != now {
			produced = append(produced, path)
		}
	}
	sort.Strings(produced)
	return produced
}

func collectArtifactsByProduced(workDir string, produced []string) ([]scriptcontract.Artifact, []string) {
	artifacts := make([]scriptcontract.Artifact, 0, len(produced))
	warnings := make([]string, 0)
	for _, rel := range produced {
		path := filepath.Join(workDir, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		ok, kind, reason := gate.CheckDelivery(path)
		artifact := scriptcontract.Artifact{Name: filepath.Base(path), Path: path, Size: info.Size(), Kind: kind, OK: ok, Reason: reason}
		artifacts = append(artifacts, artifact)
		if !ok {
			warnings = append(warnings, artifact.Name+": "+reason)
		}
	}
	return artifacts, warnings
}

func collectRemoteArtifactsByProduced(ctx context.Context, sess *sandboxsession.Session, artifactRoot string, produced []string) ([]scriptcontract.Artifact, []string) {
	artifacts := make([]scriptcontract.Artifact, 0, len(produced))
	warnings := make([]string, 0)
	root, err := boundaryPath(artifactRoot)
	if err != nil {
		return artifacts, []string{"artifact_root: " + err.Error()}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return artifacts, []string{"artifact_root: " + err.Error()}
	}
	for _, rel := range produced {
		remotePath := normalizeSlash(rel)
		if remotePath == "" || remotePath == "." {
			continue
		}
		stat, err := sess.Stat(ctx, remotePath)
		if err != nil || stat == nil || stat.Type == fsmodel.EntryTypeDir {
			continue
		}
		data, err := sess.ReadFile(ctx, remotePath, fscontract.ReadOptions{})
		if err != nil {
			warnings = append(warnings, remotePath+": "+err.Error())
			continue
		}
		localPath := filepath.Join(root, filepath.FromSlash(remotePath))
		localPath, err = boundaryPath(localPath)
		if err != nil || !isWithinPath(localPath, root) {
			warnings = append(warnings, remotePath+": artifact path outside root")
			continue
		}
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			warnings = append(warnings, remotePath+": "+err.Error())
			continue
		}
		if err := os.WriteFile(localPath, data, 0o644); err != nil {
			warnings = append(warnings, remotePath+": "+err.Error())
			continue
		}
		ok, kind, reason := gate.CheckDelivery(localPath)
		artifact := scriptcontract.Artifact{Name: filepath.Base(localPath), Path: localPath, Size: int64(len(data)), Kind: kind, OK: ok, Reason: reason}
		artifacts = append(artifacts, artifact)
		if !ok {
			warnings = append(warnings, artifact.Name+": "+reason)
		}
	}
	return artifacts, warnings
}

func sanitize(v string) string {
	v = strings.TrimSpace(v)
	replacer := strings.NewReplacer(`/`, `_`, `\\`, `_`, `:`, `_`, `*`, `_`, `?`, `_`, `"`, `_`, `<`, `_`, `>`, `_`, `|`, `_`)
	return replacer.Replace(v)
}

func boundaryPath(pathValue string) (string, error) {
	abs, err := filepath.Abs(pathValue)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(real), nil
	}
	info, err := os.Lstat(abs)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("无法解析符号链接: %s", pathValue)
	}
	return abs, nil
}

func isWithinPath(pathValue, root string) bool {
	pathValue = filepath.Clean(pathValue)
	root = filepath.Clean(root)
	if samePath(pathValue, root) {
		return true
	}
	rel, err := filepath.Rel(root, pathValue)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
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
	parts := make([]string, 0, 8)
	seen := map[string]struct{}{}
	addNodeModuleAncestors := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" {
			return
		}
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
		for {
			nodeModules := filepath.Clean(filepath.Join(root, "node_modules"))
			if _, ok := seen[nodeModules]; !ok {
				seen[nodeModules] = struct{}{}
				parts = append(parts, nodeModules)
			}
			parent := filepath.Dir(root)
			if parent == root {
				break
			}
			root = parent
		}
	}
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}
	addNodeModuleAncestors(root)
	if cwd, err := os.Getwd(); err == nil {
		addNodeModuleAncestors(cwd)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		legacy := filepath.Join(home, ".node_modules")
		if _, ok := seen[legacy]; !ok {
			parts = append(parts, legacy)
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func materializeResultArtifacts(outputDir string, resultArtifacts []execmodel.Artifact) ([]scriptcontract.Artifact, []string) {
	if len(resultArtifacts) == 0 {
		return nil, nil
	}
	out := make([]scriptcontract.Artifact, 0, len(resultArtifacts))
	warnings := make([]string, 0)
	outputAbs, err := boundaryPath(outputDir)
	if err != nil {
		return nil, []string{"解析输出目录失败: " + err.Error()}
	}
	if err := os.MkdirAll(outputAbs, 0o755); err != nil {
		return nil, []string{"创建输出目录失败: " + err.Error()}
	}
	for _, artifact := range resultArtifacts {
		if strings.TrimSpace(artifact.LocalPath) == "" {
			continue
		}
		local, warning := materializeResultArtifact(outputAbs, artifact)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if local.Name != "" {
			out = append(out, local)
		}
	}
	return out, warnings
}

func materializeResultArtifact(outputDir string, artifact execmodel.Artifact) (scriptcontract.Artifact, string) {
	name := safeArtifactName(firstNonEmpty(artifact.Name, filepath.Base(artifact.LocalPath)))
	src, err := boundaryPath(artifact.LocalPath)
	if err != nil {
		return scriptcontract.Artifact{}, fmt.Sprintf("%s: 解析执行产物失败: %v", name, err)
	}
	if !isWithinPath(src, outputDir) {
		dest := filepath.Join(outputDir, name)
		if err := copyFile(src, dest); err != nil {
			return scriptcontract.Artifact{}, fmt.Sprintf("%s: 同步执行产物失败: %v", name, err)
		}
		src = dest
	}
	info, err := os.Stat(src)
	if err != nil {
		return scriptcontract.Artifact{}, fmt.Sprintf("%s: 读取产物信息失败: %v", name, err)
	}
	ok, kind, reason := gate.CheckDelivery(src)
	local := scriptcontract.Artifact{Name: filepath.Base(src), Path: src, Size: info.Size(), Kind: kind, OK: ok, Reason: reason}
	if !ok {
		return local, local.Name + ": " + reason
	}
	return local, ""
}

func safeArtifactName(raw string) string {
	name := filepath.Base(filepath.FromSlash(strings.TrimSpace(raw)))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "artifact"
	}
	return name
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, copyErr := out.ReadFrom(in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
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
