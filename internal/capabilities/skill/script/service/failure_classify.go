package service

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	skillmodel "genesis-agent/internal/capabilities/skill/model"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
)

var (
	reModuleNotFound      = regexp.MustCompile(`(?i)ModuleNotFoundError:\s*No module named ['"]([^'"]+)['"]`)
	reCannotFindMod       = regexp.MustCompile(`(?i)Cannot find module ['"]([^'"]+)['"]`)
	reNpmNotFound         = regexp.MustCompile(`(?i)Error:\s*Cannot find package ['"]([^'"]+)['"]`)
	rePythonOpenFile      = regexp.MustCompile(`(?i)(?:python(?:\.exe)?|[^\s]+python[^\s]*)?:?\s*can't open file ['"]([^'"]+)['"]`)
	reSandboxInputMissing = regexp.MustCompile(`(?i)(?:FileNotFoundError|No such file or directory).*['"](/workspace/input/[^'"]+)['"]`)
)

// classifyFailure 解析脚本 hint / 常见 stderr，填充 failure_kind 与 suggested_*。
// 对齐 Codex image_gen._dependency_hint：脚本侧给出可行动提示，平台结构化回传。
// 不变量：凡 OK=false 最终必须有非空 FailureKind（便于模型分流）。
func classifyFailure(out *scriptcontract.RunResult) {
	if out == nil || out.OK {
		return
	}
	if out.FailureKind == "" {
		if kind := detectArtifactFailure(out); kind != "" {
			out.FailureKind = kind
		}
	}
	if out.FailureKind == "" {
		if kind := detectApprovalOrTimeout(out.Error); kind != "" {
			out.FailureKind = kind
		}
	}
	if out.FailureKind == "" {
		if kind := detectSandboxInputMissing(out.Stderr); kind != "" {
			out.FailureKind = kind
		}
	}
	hint, dep, mgr := parseScriptHint(out.Stdout)
	if out.FailureKind == "" && hint != "" {
		out.FailureKind = hint
	}
	if out.FailureKind == "" {
		if kind, name, manager := detectStderrDependency(out.Stderr, out.Script); kind != "" {
			out.FailureKind = kind
			if dep == "" {
				dep = name
			}
			if mgr == "" {
				mgr = manager
			}
			if len(out.Missing) == 0 && name != "" {
				require := name
				if manager == "system" {
					if _, req := detectSystemBinaryMissing(out.Stderr); req != "" {
						require = req
					}
				}
				out.Missing = []scriptcontract.MissingDep{{
					Manager: manager,
					Name:    name,
					Require: require,
				}}
			}
		}
	}
	// Error/Stdout 也可能带系统二进制缺失（Python FileNotFoundError 常被包装进 Error）。
	if out.FailureKind == "" || (out.FailureKind == "script_error" && len(out.Missing) == 0) {
		blob := out.Error + "\n" + out.Stdout + "\n" + out.Stderr
		if name, require := detectSystemBinaryMissing(blob); name != "" {
			out.FailureKind = "dependency_missing"
			if dep == "" {
				dep = name
			}
			if mgr == "" {
				mgr = "system"
			}
			if len(out.Missing) == 0 {
				out.Missing = []scriptcontract.MissingDep{{
					Manager: "system",
					Name:    name,
					Require: require,
				}}
			}
		}
	}
	// 凡失败路径都必须有 kind：含 runner err、入口禁用等 ExitCode 仍为 0 的情况。
	if out.FailureKind == "" {
		out.FailureKind = "script_error"
	}
	if out.FailureKind == "dependency_missing" {
		if len(out.Missing) == 0 && dep != "" {
			manager := mgr
			if manager == "" {
				manager = guessManager(out.Script)
			}
			out.Missing = []scriptcontract.MissingDep{{
				Manager: manager,
				Name:    dep,
				Require: dep,
			}}
		}
		onlySystem := len(out.Missing) > 0
		hasInstallable := false
		for _, m := range out.Missing {
			if m.Manager == "npm" || m.Manager == "pip" {
				hasInstallable = true
				onlySystem = false
			} else if m.Manager != "system" {
				onlySystem = false
			}
		}
		if onlySystem && !hasInstallable {
			if out.SuggestedAction == "" {
				out.SuggestedAction = "use_preinstalled_image_or_local_toolchain"
			}
			out.Retryable = false
		} else {
			if out.SuggestedAction == "" {
				out.SuggestedAction = "install_then_retry"
			}
			out.Retryable = true
		}
		if out.SuggestedInstall == nil {
			out.SuggestedInstall = buildSuggestedInstall(out.Skill, out.Missing)
		}
	}
	if out.FailureKind == "dependency_install_forbidden" {
		if out.SuggestedAction == "" {
			out.SuggestedAction = "use_declared_runtime_profile_or_install_skill_dependencies"
		}
		out.Retryable = false
	}
	if out.FailureKind == "sandbox_violation" {
		out.SuggestedAction = "escalate_or_change_sandbox"
		out.Retryable = true
	}
	if out.FailureKind == "artifact_invalid" {
		out.SuggestedAction = "fix_script_or_avoid_fake_write"
		out.Retryable = false
	}
	if out.FailureKind == "approval_denied" {
		out.SuggestedAction = "explain_to_user"
		out.Retryable = false
	}
	if out.FailureKind == "timeout" {
		out.SuggestedAction = "increase_timeout_or_split"
		out.Retryable = true
	}
	if out.FailureKind == "script_entry_missing" {
		out.SuggestedAction = "check_skill_script_staging_or_sandbox_working_dir"
		out.Retryable = false
	}
	if out.FailureKind == "sandbox_input_missing" {
		out.SuggestedAction = "check_sandbox_input_artifact_transport"
		out.Retryable = false
	}
	if out.FailureKind == "sandbox_unavailable" {
		out.SuggestedAction = "stop_and_report_sandbox_unavailable"
		out.Retryable = false
	}
}

func classifyFailureForSkill(out *scriptcontract.RunResult, deps skillmodel.Dependencies) {
	classifyFailure(out)
	constrainInstallSuggestionToDeclaredRuntime(out, deps)
}

func constrainInstallSuggestionToDeclaredRuntime(out *scriptcontract.RunResult, deps skillmodel.Dependencies) {
	if out == nil || out.FailureKind != "dependency_missing" || len(out.Missing) == 0 {
		return
	}
	undeclared := make([]string, 0)
	for _, missing := range out.Missing {
		if missing.Manager != "npm" && missing.Manager != "pip" {
			continue
		}
		if !runtimeDependencyDeclared(missing, deps) {
			undeclared = append(undeclared, missing.Manager+":"+missing.Name)
		}
	}
	if len(undeclared) == 0 {
		return
	}
	out.SuggestedInstall = nil
	out.SuggestedAction = "rewrite_script_use_declared_dependencies"
	out.Retryable = true
	out.Warnings = append(out.Warnings, "dependency_not_declared: "+strings.Join(undeclared, ","))
}

func runtimeDependencyDeclared(missing scriptcontract.MissingDep, deps skillmodel.Dependencies) bool {
	manager := strings.ToLower(strings.TrimSpace(missing.Manager))
	name := strings.ToLower(strings.TrimSpace(missing.Name))
	require := strings.ToLower(strings.TrimSpace(missing.Require))
	if require == "" {
		require = name
	}
	switch manager {
	case "npm":
		for _, dep := range deps.Runtime.Node {
			if runtimeNameMatches(name, require, dep.Name, dep.Require) {
				return true
			}
		}
	case "pip":
		for _, dep := range deps.Runtime.Python {
			if runtimeNameMatches(name, require, dep.Name, dep.Import) {
				return true
			}
		}
	}
	return false
}

func runtimeNameMatches(name, require, declaredName, declaredImport string) bool {
	declaredName = strings.ToLower(strings.TrimSpace(declaredName))
	declaredImport = strings.ToLower(strings.TrimSpace(declaredImport))
	return (declaredName != "" && (name == declaredName || require == declaredName)) ||
		(declaredImport != "" && (name == declaredImport || require == declaredImport))
}
func detectApprovalOrTimeout(errMsg string) string {
	msg := strings.ToLower(strings.TrimSpace(errMsg))
	if msg == "" {
		return ""
	}
	if strings.Contains(errMsg, ErrCommandInlineRisky) {
		return "command_inline_risky"
	}
	if strings.Contains(errMsg, ErrCommandLogicalPrefix) {
		return "command_logical_prefix_forbidden"
	}
	if strings.Contains(errMsg, ErrInputPathNamespaceMismatch) {
		return "input_path_namespace_mismatch"
	}
	if strings.HasPrefix(msg, "approval ") || strings.Contains(msg, "approval denied") || strings.Contains(msg, "decisiondenied") {
		return "approval_denied"
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "context deadline") {
		return "timeout"
	}
	if strings.Contains(msg, "sandbox_unavailable") ||
		strings.Contains(msg, "sandbox session") ||
		strings.Contains(msg, "创建沙箱失败") ||
		strings.Contains(msg, "创建 docker 容器失败") ||
		strings.Contains(msg, "docker daemon") ||
		strings.Contains(msg, "connection refused") {
		return "sandbox_unavailable"
	}
	return ""
}

func detectArtifactFailure(out *scriptcontract.RunResult) string {
	if out == nil {
		return ""
	}
	if strings.Contains(out.Error, "artifact gate failed") {
		return "artifact_invalid"
	}
	for _, art := range out.Artifacts {
		ext := strings.ToLower(filepath.Ext(art.Name))
		if (ext == ".pptx" || ext == ".docx" || ext == ".xlsx" || ext == ".pdf") && !art.OK {
			return "artifact_invalid"
		}
	}
	return ""
}

func detectSandboxInputMissing(stderr string) string {
	if reSandboxInputMissing.MatchString(stderr) {
		return "sandbox_input_missing"
	}
	return ""
}

func parseScriptHint(stdout string) (hint, dependency, manager string) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return "", "", ""
	}
	// 允许前后有非 JSON 噪声：取首个 '{' 起尝试解析。
	if i := strings.Index(stdout, "{"); i >= 0 {
		stdout = stdout[i:]
	}
	if !strings.HasPrefix(stdout, "{") {
		return "", "", ""
	}
	var payload map[string]any
	if json.Unmarshal([]byte(stdout), &payload) != nil {
		return "", "", ""
	}
	if h, ok := payload["hint"].(string); ok {
		hint = strings.TrimSpace(h)
	}
	if d, ok := payload["dependency"].(string); ok {
		dependency = strings.TrimSpace(d)
	}
	if m, ok := payload["manager"].(string); ok {
		manager = strings.TrimSpace(m)
	}
	return hint, dependency, manager
}

func detectStderrDependency(stderr, script string) (kind, name, manager string) {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return "", "", ""
	}
	if m := reModuleNotFound.FindStringSubmatch(stderr); len(m) == 2 {
		return "dependency_missing", m[1], "pip"
	}
	if m := reCannotFindMod.FindStringSubmatch(stderr); len(m) == 2 {
		missing := strings.TrimSpace(m[1])
		if isMissingScriptEntry(missing, script) {
			return "script_entry_missing", "", ""
		}
		name = strings.TrimSuffix(filepath.ToSlash(missing), ".js")
		parts := strings.Split(name, "/")
		if len(parts) > 0 && strings.HasPrefix(parts[0], "@") && len(parts) > 1 {
			name = parts[0] + "/" + parts[1]
		} else if len(parts) > 0 {
			name = parts[0]
		}
		return "dependency_missing", name, "npm"
	}
	if m := rePythonOpenFile.FindStringSubmatch(stderr); len(m) == 2 && isMissingScriptEntry(m[1], script) {
		return "script_entry_missing", "", ""
	}
	if m := reNpmNotFound.FindStringSubmatch(stderr); len(m) == 2 {
		return "dependency_missing", m[1], "npm"
	}
	if name, _ := detectSystemBinaryMissing(stderr); name != "" {
		return "dependency_missing", name, "system"
	}
	return "", "", ""
}

// detectSystemBinaryMissing 识别常见系统二进制缺失（三端 stderr 文案差异大，按关键词统一归类）。
// 不绑定技能名；仅覆盖 Office/文档链路常见工具，避免把普通 FileNotFound 误判为 system 依赖。
func detectSystemBinaryMissing(text string) (name, require string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	lower := strings.ToLower(text)
	missingSignal := strings.Contains(lower, "not found") ||
		strings.Contains(lower, "is not recognized") ||
		strings.Contains(lower, "command not found") ||
		strings.Contains(lower, "no such file") ||
		strings.Contains(lower, "无法将") // Windows PowerShell 常见
	if !missingSignal {
		return "", ""
	}
	type sysBin struct {
		name, require string
		needles       []string
	}
	for _, b := range []sysBin{
		{name: "libreoffice", require: "soffice", needles: []string{"soffice", "libreoffice"}},
		{name: "poppler", require: "pdftoppm", needles: []string{"pdftoppm", "poppler"}},
	} {
		for _, n := range b.needles {
			if strings.Contains(lower, n) {
				return b.name, b.require
			}
		}
	}
	return "", ""
}

func isMissingScriptEntry(missing, script string) bool {
	missing = strings.TrimSpace(filepath.ToSlash(missing))
	if missing == "" {
		return false
	}
	if !strings.Contains(missing, "/") && !strings.HasPrefix(missing, ".") {
		return false
	}
	scriptBase := filepath.Base(filepath.ToSlash(script))
	if scriptBase == "." || scriptBase == "" {
		return false
	}
	return filepath.Base(missing) == scriptBase
}

// guessManager 仅按脚本扩展名推断；禁止用包名模糊猜（避免 jsonschema 等误判 npm）。
func guessManager(script string) string {
	switch strings.ToLower(filepath.Ext(script)) {
	case ".js", ".mjs", ".cjs", ".ts":
		return "npm"
	case ".py":
		return "pip"
	default:
		return ""
	}
}

func buildSuggestedInstall(skill string, missing []scriptcontract.MissingDep) *scriptcontract.SuggestedInstall {
	if len(missing) == 0 {
		return &scriptcontract.SuggestedInstall{
			Tool: "install_skill_dependencies",
			Args: map[string]any{"skill": skill},
		}
	}
	packages := make([]map[string]string, 0, len(missing))
	fallbackParts := make([]string, 0, len(missing))
	onlySystem := true
	for _, m := range missing {
		if m.Manager != "system" {
			onlySystem = false
		}
		pkg := map[string]string{"name": m.Name}
		if m.Manager != "" {
			pkg["manager"] = m.Manager
		}
		// system 不可对话期安装，不塞进 install 工具参数，避免 Agent 空转。
		if m.Manager == "npm" || m.Manager == "pip" {
			packages = append(packages, pkg)
		}
		switch m.Manager {
		case "npm":
			fallbackParts = append(fallbackParts, "npm install "+m.Name)
		case "pip":
			fallbackParts = append(fallbackParts, "pip install "+m.Name)
		case "system":
			cmd := m.Require
			if cmd == "" {
				cmd = m.Name
			}
			fallbackParts = append(fallbackParts, "ensure system binary on PATH/image: "+cmd)
		default:
			if m.Name != "" {
				fallbackParts = append(fallbackParts, "install "+m.Name)
			}
		}
	}
	if onlySystem || len(packages) == 0 {
		return &scriptcontract.SuggestedInstall{
			ShellFallback: strings.Join(fallbackParts, " && "),
		}
	}
	return &scriptcontract.SuggestedInstall{
		Tool: "install_skill_dependencies",
		Args: map[string]any{
			"skill":    skill,
			"packages": packages,
		},
		ShellFallback: strings.Join(fallbackParts, " && "),
	}
}
