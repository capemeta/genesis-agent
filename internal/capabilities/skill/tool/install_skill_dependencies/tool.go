package install_skill_dependencies

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

const (
	toolName       = "install_skill_dependencies"
	defaultTimeout = 5 * time.Minute
)

// 包名白名单字符：对齐 npm/pypi 常见命名，拒绝 shell 元字符。
// pip 允许 extras：markitdown[pptx]；白名单按 base name 匹配。
var (
	safePackageName    = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9@._+\-/]*$`)
	safePipPackageSpec = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9@._+\-/]*(\[[A-Za-z0-9][A-Za-z0-9,_]*\])?$`)
)

// SkillResolver 解析 Skill 元数据（产品注入完整 skill.Service 即可）。
type SkillResolver interface {
	Resolve(ctx context.Context, req skillcontract.ResolveRequest) (model.ResolvedInvocation, error)
}

// Deps 是 install_skill_dependencies 依赖。
// 形态借鉴 Codex mcp_skill_dependencies：确认 → 执行 → 落点可被后续 run 看见；
// 审批对齐 Kode：npm install 不在 SAFE → 默认 ask。
type Deps struct {
	Skills         SkillResolver
	Runner         execcontract.ExecutionRunner
	Approval       approvalcontract.Service
	CatalogRequest skillcontract.CatalogRequest
	Sandbox        execmodel.SandboxProfile
	WorkspaceRoot  string
}

// Tool 在约定作用域安装 Skill 声明的 runtime 包（不执行业务脚本）。
type Tool struct {
	deps Deps
}

type packageInput struct {
	Manager string `json:"manager"`
	Name    string `json:"name"`
}

type input struct {
	Skill    string         `json:"skill"`
	Packages []packageInput `json:"packages"`
	Scope    string         `json:"scope,omitempty"`
}

type resultPayload struct {
	OK         bool              `json:"ok"`
	Skill      string            `json:"skill"`
	Scope      string            `json:"scope"`
	Commands   []string          `json:"commands,omitempty"`
	Installed  []packageInput    `json:"installed,omitempty"`
	ExitCode   int               `json:"exit_code,omitempty"`
	Stdout     string            `json:"stdout,omitempty"`
	Stderr     string            `json:"stderr,omitempty"`
	Error      string            `json:"error,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Suggested  string            `json:"suggested_next,omitempty"`
	DurationMS int64             `json:"duration_ms,omitempty"`
}

// New 创建工具。
func New(deps Deps) (tool.Tool, error) {
	if deps.Skills == nil {
		return nil, fmt.Errorf("skill service未配置")
	}
	if deps.Runner == nil {
		return nil, fmt.Errorf("execution runner未配置")
	}
	if deps.Approval == nil {
		return nil, fmt.Errorf("approval service未配置")
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name: toolName,
		Description: strings.TrimSpace(`
安装 Skill 在 dependencies.runtime 中声明的第三方包（npm/pip）。
不执行业务脚本；安装成功后须用相同参数再调 run_skill_command。
仅安装该 skill 已在 dependencies.runtime 中声明的包；禁止任意 shell。scope=image 时返回需重建镜像。
`),
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"skill": {Type: "string", Description: "Skill 名称，例如 office-ppt"},
				"packages": {
					Type:        "array",
					Description: "要安装的包列表",
					Items: &tool.ParameterSchema{
						Type: "object",
						Properties: map[string]*tool.ParameterSchema{
							"manager": {Type: "string", Description: "npm 或 pip"},
							"name":    {Type: "string", Description: "包名"},
						},
						Required: []string{"manager", "name"},
					},
				},
				"scope": {Type: "string", Description: "当前仅支持 workspace；session/user/image 见后续 Gate"},
			},
			Required: []string{"skill", "packages"},
		},
		Traits: tool.ToolTraits{
			Exposure:        tool.ToolExposureDirect,
			ReadOnly:        false,
			ConcurrencySafe: false,
			NeedsPermission: true,
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	started := time.Now()
	var in input
	if err := toolparam.Decode(params, &in); err != nil {
		return "", fmt.Errorf("解析install_skill_dependencies参数失败: %w", err)
	}
	skillName := strings.TrimSpace(in.Skill)
	if skillName == "" {
		return "", fmt.Errorf("skill不能为空")
	}
	if len(in.Packages) == 0 {
		return "", fmt.Errorf("packages不能为空")
	}
	binding, bound := skillcontract.InvocationBindingFromContext(ctx)
	if !bound {
		return "", fmt.Errorf("SKILL_INVOCATION_BINDING_REQUIRED: install_skill_dependencies只能在已激活Invocation内执行")
	}
	if err := skillcontract.ValidateBoundTarget(binding, skillName, ""); err != nil {
		return "", err
	}
	scope := strings.ToLower(strings.TrimSpace(in.Scope))
	if scope == "" {
		scope = "workspace"
	}

	out := &resultPayload{
		Skill: skillName,
		Scope: scope,
		Metadata: map[string]string{
			"skill_dep_install": "true",
			"scope":             scope,
		},
		Suggested: "install 完成后用相同参数再调用 run_skill_command",
	}

	if scope == "image" {
		out.OK = false
		out.Error = "scope=image 不允许对话期安装；请重建 office-basic 等 profile/镜像"
		out.Metadata["failure_kind"] = "install_forbidden_use_image"
		out.DurationMS = time.Since(started).Milliseconds()
		return marshalFail(out, out.Error)
	}
	// Gate B：仅 workspace。session/user 跨 backend 落点属 Gate C，避免「接受了却装到错误位置」。
	if scope != "workspace" {
		out.OK = false
		out.Error = fmt.Sprintf("scope=%s 尚未支持；当前仅支持 workspace（session/user 见 Gate C）", scope)
		out.Metadata["failure_kind"] = "install_scope_unsupported"
		out.DurationMS = time.Since(started).Milliseconds()
		return marshalFail(out, out.Error)
	}
	if remoteSandboxWorkspaceInstallUnsupported(t.deps.Sandbox) {
		out.OK = false
		out.Error = "scope=workspace 在 genesis-sandbox 远程模式下不会被后续 run_skill_command 的 office/skill session 可靠看见；当前请使用 profile/镜像预装，或等待 session scope 安装闭环"
		out.Metadata["failure_kind"] = "install_scope_not_visible"
		out.Metadata["provider"] = t.deps.Sandbox.Provider
		out.Metadata["sandbox_mode"] = string(t.deps.Sandbox.Mode)
		out.Suggested = "不要重复安装；请重跑脚本以使用镜像预装依赖，或重建对应 runtime profile"
		out.DurationMS = time.Since(started).Milliseconds()
		return marshalFail(out, out.Error)
	}

	meta := model.Metadata{Name: binding.PhysicalSkill, Authority: binding.Package.Authority, PackageID: binding.Package.PackageID, Version: binding.Package.Version}.Normalize()
	pkgs, err := normalizeAndAuthorizePackages(in.Packages, binding.RuntimeProfile.Dependencies)
	if err != nil {
		out.OK = false
		out.Error = err.Error()
		out.Metadata["failure_kind"] = "package_not_allowed"
		out.DurationMS = time.Since(started).Milliseconds()
		return marshalFail(out, out.Error)
	}

	wsRoot := strings.TrimSpace(t.deps.WorkspaceRoot)
	if wsRoot == "" {
		return "", fmt.Errorf("安装 Skill 依赖缺少显式 workspace root")
	}
	absRoot, err := filepath.Abs(wsRoot)
	if err != nil {
		return "", fmt.Errorf("解析工作区失败: %w", err)
	}
	installRoot := skillDependencyInstallRoot(absRoot, meta)
	steps, err := buildInstallSteps(pkgs, installRoot)
	if err != nil {
		return "", err
	}
	out.Commands = installStepCommands(steps)
	out.Installed = pkgs
	out.Metadata["install_root"] = installRoot

	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{
		ToolName: toolName,
		Action:   approvalmodel.ActionCommandExec,
		Resource: approvalmodel.Resource{
			Type:    "skill_dep_install",
			URI:     "Skill(" + binding.Handle + ")+install_dependencies",
			Display: "安装 " + meta.Name + " 依赖: " + joinPkgNames(pkgs),
			Metadata: map[string]string{
				"skill_dep_install": "true",
				"skill":             meta.Name,
				"scope":             scope,
				"packages":          joinPkgNames(pkgs),
				"dangerous":         "true",
				"network":           "registry",
			},
		},
		Reason: "安装 Skill runtime 依赖（npm/pip）；",
		Risk:   approvalmodel.RiskHigh,
		SuggestedScopes: []approvalmodel.GrantScope{
			approvalmodel.GrantScopeOnce,
			approvalmodel.GrantScopeSession,
		},
		Metadata: map[string]string{
			"skill_dep_install": "true",
			"commands":          strings.Join(out.Commands, " && "),
		},
	})
	if err != nil {
		return "", err
	}
	if decision.Type != approvalmodel.DecisionApproved && decision.Type != approvalmodel.DecisionApprovedForScope {
		out.OK = false
		out.Error = fmt.Sprintf("approval %s: %s", decision.Type, decision.Reason)
		out.Metadata["failure_kind"] = "approval_denied"
		out.DurationMS = time.Since(started).Milliseconds()
		return marshalFail(out, out.Error)
	}

	for _, dir := range installTargetDirs(pkgs, installRoot) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("创建依赖安装目录失败: %w", err)
		}
	}
	control, ok := workcontract.ControlPlaneFromContext(ctx)
	if !ok {
		return "", execcontract.NewError(execcontract.ErrCodeExecutionBindingRequired, fmt.Errorf("安装 Skill 依赖缺少 workspace control plane"))
	}
	execution, err := control.PrepareExecution(ctx, workcontract.PrepareExecutionRequest{Subject: execmodel.ExecutionSubjectRef{TaskID: "skill-deps:" + meta.Name}, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeProject, HasProject: true}, RequestedAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		return "", fmt.Errorf("准备 Skill 依赖 execution: %w", err)
	}
	if err := ensurePathWithinWorkspace(installRoot, execution.Workspace.WorkDir); err != nil {
		return "", err
	}

	sandbox := cloneSandbox(t.deps.Sandbox)
	// Gate B 最小可用：安装走独立元数据标记 + build Operation；
	// 远端/build profile 完整消费属 Gate C。本地 disabled 时仍需审批+命令白名单。
	sandbox.Operation = execmodel.SandboxOperationBuildDependencies
	sandbox.RuntimeProfile = execmodel.RuntimeProfileSkillBuildPolyglot
	if sandbox.Metadata == nil {
		sandbox.Metadata = map[string]string{}
	}
	sandbox.Metadata["skill_dep_install"] = "true"
	sandbox.Metadata["source"] = "skill"
	sandbox.Metadata["skill_id"] = meta.Name
	sandbox.Metadata["build_dependencies"] = "true"
	sandbox.Metadata["todo_build_profile"] = "true" // 完整 build profile 网络 allowlist 仍待 sandbox 仓/运维落地
	out.Metadata["runtime_profile"] = string(sandbox.RuntimeProfile)
	out.Metadata["operation"] = string(sandbox.Operation)

	var stdoutBuf, stderrBuf strings.Builder
	lastCode := 0
	// cwd 相对安装：禁止 npm --prefix / pip --target 拼绝对路径引号（Windows cmd 会弄坏路径）。
	// 远程 genesis-sandbox 在上方已 early-return，不会进入此循环。
	for _, step := range steps {
		result, runErr := t.deps.Runner.Run(ctx, execmodel.Command{
			Command: step.Command,
			Cwd:     step.Cwd,
			Shell:   "auto",
		}, execcontract.RunOptions{
			Timeout:   defaultTimeout,
			Sandbox:   sandbox,
			Binding:   execution.Binding,
			Workspace: execution.Workspace,
		})
		if runErr != nil {
			out.OK = false
			out.Error = runErr.Error()
			out.Stdout = stdoutBuf.String()
			out.Stderr = stderrBuf.String()
			out.DurationMS = time.Since(started).Milliseconds()
			return marshalFail(out, out.Error)
		}
		if result == nil {
			out.OK = false
			out.Error = "execution runner返回空结果"
			out.DurationMS = time.Since(started).Milliseconds()
			return marshalFail(out, out.Error)
		}
		if result.Stdout != "" {
			stdoutBuf.WriteString(result.Stdout)
			if !strings.HasSuffix(result.Stdout, "\n") {
				stdoutBuf.WriteByte('\n')
			}
		}
		if result.Stderr != "" {
			stderrBuf.WriteString(result.Stderr)
			if !strings.HasSuffix(result.Stderr, "\n") {
				stderrBuf.WriteByte('\n')
			}
		}
		lastCode = result.ExitCode
		if result.ExitCode != 0 {
			out.OK = false
			out.ExitCode = result.ExitCode
			out.Stdout = stdoutBuf.String()
			out.Stderr = stderrBuf.String()
			out.Error = fmt.Sprintf("install exit_code=%d for %q (cwd=%s)", result.ExitCode, step.Command, step.Cwd)
			if result.Error != "" {
				out.Error = result.Error
			}
			out.DurationMS = time.Since(started).Milliseconds()
			return marshalFail(out, out.Error)
		}
	}
	out.Metadata["run_id"] = execution.Binding.Owner.RunID
	out.ExitCode = lastCode
	out.Stdout = stdoutBuf.String()
	out.Stderr = stderrBuf.String()
	out.OK = true
	out.DurationMS = time.Since(started).Milliseconds()
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func ensurePathWithinWorkspace(candidate, workspaceRoot string) error {
	rootReal, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return fmt.Errorf("解析依赖 workspace root: %w", err)
	}
	candidateReal, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return fmt.Errorf("解析依赖安装目录: %w", err)
	}
	rel, err := filepath.Rel(rootReal, candidateReal)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("依赖安装目录越过控制面 project workspace")
	}
	return nil
}

func normalizeAndAuthorizePackages(in []packageInput, deps model.Dependencies) ([]packageInput, error) {
	wl := deps.RuntimeWhitelist()
	if len(wl) == 0 {
		return nil, fmt.Errorf("skill 未声明 dependencies.runtime，拒绝对话期装包")
	}
	out := make([]packageInput, 0, len(in))
	seen := map[string]struct{}{}
	for _, p := range in {
		manager := normalizeManager(p.Manager)
		name := strings.TrimSpace(p.Name)
		if manager == "" || name == "" {
			return nil, fmt.Errorf("packages 项必须含 manager 与 name")
		}
		if manager == "system" {
			return nil, fmt.Errorf("system 依赖（如 libreoffice）不可对话期安装，请使用预装镜像/profile")
		}
		baseName := name
		if manager == "pip" {
			if !isSafePipPackageSpec(name) {
				return nil, fmt.Errorf("非法包名: %q", name)
			}
			baseName = pipBaseName(name)
		} else if !isSafePackageName(name) {
			return nil, fmt.Errorf("非法包名: %q", name)
		}
		key := manager + ":" + strings.ToLower(baseName)
		if _, ok := wl[key]; !ok {
			return nil, fmt.Errorf("包 %s/%s 未在该 skill 的 dependencies.runtime 中声明，本次安装被拒绝；可安装已声明包后重跑，或按 skill 文档用已声明能力改实现，或先扩展该 skill 的 dependencies.runtime 声明后再安装", manager, baseName)
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, packageInput{Manager: manager, Name: name})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("packages 归一化后为空")
	}
	return out, nil
}

func normalizeManager(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "npm", "node":
		return "npm"
	case "pip", "python", "uv":
		return "pip"
	case "system", "bin", "command":
		return "system"
	default:
		return ""
	}
}

func isSafePackageName(name string) bool {
	if !safePackageName.MatchString(name) {
		return false
	}
	if strings.ContainsAny(name, " \t\n;&|`$<>(){}") {
		return false
	}
	// 拒绝路径穿越与绝对路径片段。
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return false
	}
	if len(name) >= 2 && name[1] == ':' { // Windows 盘符
		return false
	}
	return true
}

func isSafePipPackageSpec(name string) bool {
	if !safePipPackageSpec.MatchString(name) {
		return false
	}
	if strings.ContainsAny(name, " \t\n;&|`$<>(){}") {
		return false
	}
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return false
	}
	if len(name) >= 2 && name[1] == ':' {
		return false
	}
	return true
}

func pipBaseName(name string) string {
	name = strings.TrimSpace(name)
	if idx := strings.Index(name, "["); idx > 0 {
		return name[:idx]
	}
	return name
}

// installStep 是本地 L0 / 本地平台共用的安装步骤：cwd 指向落点，命令不含绝对路径。
type installStep struct {
	Manager string
	Cwd     string
	Command string
}

func buildInstallSteps(pkgs []packageInput, installRoot string) ([]installStep, error) {
	byManager := map[string][]string{}
	for _, p := range pkgs {
		byManager[p.Manager] = append(byManager[p.Manager], p.Name)
	}
	steps := make([]installStep, 0, len(byManager)+1)
	// 固定顺序便于测试稳定。
	for _, manager := range []string{"npm", "pip"} {
		names := byManager[manager]
		if len(names) == 0 {
			continue
		}
		switch manager {
		case "npm":
			steps = append(steps, installStep{
				Manager: manager,
				Cwd:     filepath.Join(installRoot, "node"),
				Command: "npm install " + strings.Join(names, " "),
			})
		case "pip":
			venvDir := filepath.Join(installRoot, "venv")
			if !venvPythonExists(venvDir) {
				steps = append(steps, installStep{
					Manager: "venv",
					Cwd:     installRoot,
					Command: "python -m venv venv",
				})
			}
			steps = append(steps, installStep{
				Manager: manager,
				Cwd:     venvDir,
				// 引号保护 extras（markitdown[pptx]），避免 bash 字符类 glob。
				Command: venvPythonRel() + " -m pip install " + quotePipPackageArgs(names),
			})
		}
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("无法生成安装命令")
	}
	return steps, nil
}

func installStepCommands(steps []installStep) []string {
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		out = append(out, step.Command)
	}
	return out
}

func installTargetDirs(pkgs []packageInput, installRoot string) []string {
	dirs := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, p := range pkgs {
		var dir string
		switch p.Manager {
		case "npm":
			dir = filepath.Join(installRoot, "node")
		case "pip":
			dir = installRoot // venv 由 python -m venv 创建
		default:
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	return dirs
}

func quotePipPackageArgs(names []string) string {
	parts := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		parts = append(parts, quotePipPackageArg(name))
	}
	return strings.Join(parts, " ")
}

// quotePipPackageArg 处理 pip extras（如 markitdown[pptx]）的 shell 安全拼参。
// Windows cmd：双引号会原样进入 pip argv → Invalid requirement: '"pkg[extra]"'；
// cmd 不对 [] 做 glob，故不加引号。Unix bash：需要引号防止字符类 glob。
func quotePipPackageArg(name string) string {
	if !strings.ContainsAny(name, "[]") {
		return name
	}
	if runtime.GOOS == "windows" {
		return name
	}
	return `"` + name + `"`
}

func venvPythonRel() string {
	if runtime.GOOS == "windows" {
		return `Scripts\python.exe`
	}
	return "bin/python"
}

func venvPythonExists(venvDir string) bool {
	name := "python"
	if runtime.GOOS == "windows" {
		name = "python.exe"
	}
	info, err := os.Stat(filepath.Join(venvDir, venvBinDirName(), name))
	return err == nil && !info.IsDir()
}

func venvBinDirName() string {
	if runtime.GOOS == "windows" {
		return "Scripts"
	}
	return "bin"
}

func skillDependencyInstallRoot(workspaceRoot string, meta model.Metadata) string {
	skillID := sanitizePathPart(firstNonEmpty(string(meta.PackageID), meta.Name))
	return filepath.Join(workspaceRoot, ".genesis", "cache", "skill-deps", skillID)
}

func sanitizePathPart(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "skill"
	}
	replacer := strings.NewReplacer(`/`, `_`, `\`, `_`, `:`, `_`, `*`, `_`, `?`, `_`, `"`, `_`, `<`, `_`, `>`, `_`, `|`, `_`)
	value = replacer.Replace(value)
	value = strings.Trim(value, ". ")
	if value == "" {
		return "skill"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func joinPkgNames(pkgs []packageInput) string {
	parts := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		parts = append(parts, p.Manager+":"+p.Name)
	}
	return strings.Join(parts, ",")
}

func remoteSandboxWorkspaceInstallUnsupported(sandbox execmodel.SandboxProfile) bool {
	if !strings.EqualFold(strings.TrimSpace(sandbox.Provider), "genesis-sandbox") {
		return false
	}
	return sandbox.Mode == execmodel.SandboxOptional || sandbox.Mode == execmodel.SandboxRequired
}

func cloneSandbox(in execmodel.SandboxProfile) execmodel.SandboxProfile {
	out := in
	if in.Metadata != nil {
		out.Metadata = make(map[string]string, len(in.Metadata))
		for k, v := range in.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}

func marshalFail(out *resultPayload, msg string) (string, error) {
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(msg) == "" {
		msg = "install_skill_dependencies failed"
	}
	return string(data), fmt.Errorf("%s", msg)
}
