package install_skill_dependencies

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/platform/contextutil"
)

const (
	toolName       = "install_skill_dependencies"
	defaultTimeout = 5 * time.Minute
)

// 包名白名单字符：对齐 npm/pypi 常见命名，拒绝 shell 元字符。
var safePackageName = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9@._+\-/]*$`)

// SkillResolver 解析 Skill 元数据（产品注入完整 skill.Service 即可）。
type SkillResolver interface {
	Resolve(ctx context.Context, req skillcontract.ResolveRequest) (model.Metadata, error)
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
不执行业务脚本；安装成功后须用相同参数再调 run_skill_script。
默认仅允许声明白名单内的包；禁止任意 shell。scope=image 时返回需重建镜像。
`),
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"skill": {Type: "string", Description: "Skill 名称，例如 office-ppt"},
				"packages": {
					Type: "array",
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
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析install_skill_dependencies参数失败: %w", err)
	}
	skillName := strings.TrimSpace(in.Skill)
	if skillName == "" {
		return "", fmt.Errorf("skill不能为空")
	}
	if len(in.Packages) == 0 {
		return "", fmt.Errorf("packages不能为空")
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
		Suggested: "install 完成后用相同参数再调用 run_skill_script",
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

	meta, err := t.deps.Skills.Resolve(ctx, skillcontract.ResolveRequest{
		CatalogRequest: t.deps.CatalogRequest,
		Name:           skillName,
	})
	if err != nil {
		return "", err
	}

	pkgs, err := normalizeAndAuthorizePackages(in.Packages, meta.Dependencies)
	if err != nil {
		out.OK = false
		out.Error = err.Error()
		out.Metadata["failure_kind"] = "package_not_allowed"
		out.DurationMS = time.Since(started).Milliseconds()
		return marshalFail(out, out.Error)
	}

	commands, err := buildInstallCommands(pkgs)
	if err != nil {
		return "", err
	}
	out.Commands = commands
	out.Installed = pkgs

	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{
		ToolName: toolName,
		Action:   approvalmodel.ActionCommandExec,
		Resource: approvalmodel.Resource{
			Type:    "skill_dep_install",
			URI:     "Skill(" + meta.QualifiedName + ")+install_dependencies",
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
		Reason: "安装 Skill runtime 依赖（npm/pip）；对齐 Kode：装包需用户确认",
		Risk:   approvalmodel.RiskHigh,
		SuggestedScopes: []approvalmodel.GrantScope{
			approvalmodel.GrantScopeOnce,
			approvalmodel.GrantScopeSession,
		},
		Metadata: map[string]string{
			"skill_dep_install": "true",
			"commands":          strings.Join(commands, " && "),
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

	wsRoot := strings.TrimSpace(t.deps.WorkspaceRoot)
	if wsRoot == "" {
		wsRoot = "."
	}
	absRoot, err := filepath.Abs(wsRoot)
	if err != nil {
		return "", fmt.Errorf("解析工作区失败: %w", err)
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
	for _, cmdLine := range commands {
		result, runErr := t.deps.Runner.Run(ctx, execmodel.Command{
			Command: cmdLine,
			Cwd:     absRoot,
			Shell:   "auto",
		}, execcontract.RunOptions{
			Timeout: defaultTimeout,
			Sandbox: sandbox,
			Workspace: execmodel.ExecutionWorkspace{
				WorkDir: absRoot,
				TmpDir:  absRoot,
			},
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
			out.Error = fmt.Sprintf("install exit_code=%d for %q", result.ExitCode, cmdLine)
			if result.Error != "" {
				out.Error = result.Error
			}
			out.DurationMS = time.Since(started).Milliseconds()
			return marshalFail(out, out.Error)
		}
	}
	if runID, ok := contextutil.GetRunID(ctx); ok {
		out.Metadata["run_id"] = runID
	}
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
		if !isSafePackageName(name) {
			return nil, fmt.Errorf("非法包名: %q", name)
		}
		key := manager + ":" + strings.ToLower(name)
		if _, ok := wl[key]; !ok {
			return nil, fmt.Errorf("包 %s/%s 不在 skill dependencies.runtime 白名单", manager, name)
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

func buildInstallCommands(pkgs []packageInput) ([]string, error) {
	byManager := map[string][]string{}
	for _, p := range pkgs {
		byManager[p.Manager] = append(byManager[p.Manager], p.Name)
	}
	cmds := make([]string, 0, len(byManager))
	// 固定顺序便于测试稳定。
	for _, manager := range []string{"npm", "pip"} {
		names := byManager[manager]
		if len(names) == 0 {
			continue
		}
		switch manager {
		case "npm":
			// 模板白名单：仅 npm install <pkg...>，禁止额外 flags。
			cmds = append(cmds, "npm install "+strings.Join(names, " "))
		case "pip":
			cmds = append(cmds, "python -m pip install "+strings.Join(names, " "))
		}
	}
	if len(cmds) == 0 {
		return nil, fmt.Errorf("无法生成安装命令")
	}
	return cmds, nil
}

func joinPkgNames(pkgs []packageInput) string {
	parts := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		parts = append(parts, p.Manager+":"+p.Name)
	}
	return strings.Join(parts, ",")
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
