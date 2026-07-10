package service

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
)

var (
	reModuleNotFound = regexp.MustCompile(`(?i)ModuleNotFoundError:\s*No module named ['"]([^'"]+)['"]`)
	reCannotFindMod  = regexp.MustCompile(`(?i)Cannot find module ['"]([^'"]+)['"]`)
	reNpmNotFound    = regexp.MustCompile(`(?i)Error:\s*Cannot find package ['"]([^'"]+)['"]`)
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
	hint, dep := parseScriptHint(out.Stdout)
	if out.FailureKind == "" && hint != "" {
		out.FailureKind = hint
	}
	if out.FailureKind == "" {
		if kind, name, manager := detectStderrDependency(out.Stderr); kind != "" {
			out.FailureKind = kind
			if dep == "" {
				dep = name
			}
			if len(out.Missing) == 0 && name != "" {
				out.Missing = []scriptcontract.MissingDep{{
					Manager: manager,
					Name:    name,
					Require: name,
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
			manager := guessManager(out.Script)
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
}

func detectApprovalOrTimeout(errMsg string) string {
	msg := strings.ToLower(strings.TrimSpace(errMsg))
	if msg == "" {
		return ""
	}
	if strings.HasPrefix(msg, "approval ") || strings.Contains(msg, "approval denied") || strings.Contains(msg, "decisiondenied") {
		return "approval_denied"
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "context deadline") {
		return "timeout"
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

func parseScriptHint(stdout string) (hint, dependency string) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return "", ""
	}
	// 允许前后有非 JSON 噪声：取首个 '{' 起尝试解析。
	if i := strings.Index(stdout, "{"); i >= 0 {
		stdout = stdout[i:]
	}
	if !strings.HasPrefix(stdout, "{") {
		return "", ""
	}
	var payload map[string]any
	if json.Unmarshal([]byte(stdout), &payload) != nil {
		return "", ""
	}
	if h, ok := payload["hint"].(string); ok {
		hint = strings.TrimSpace(h)
	}
	if d, ok := payload["dependency"].(string); ok {
		dependency = strings.TrimSpace(d)
	}
	return hint, dependency
}

func detectStderrDependency(stderr string) (kind, name, manager string) {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return "", "", ""
	}
	if m := reModuleNotFound.FindStringSubmatch(stderr); len(m) == 2 {
		return "dependency_missing", m[1], "pip"
	}
	if m := reCannotFindMod.FindStringSubmatch(stderr); len(m) == 2 {
		name = strings.TrimSuffix(m[1], ".js")
		parts := strings.Split(name, "/")
		if len(parts) > 0 && strings.HasPrefix(parts[0], "@") && len(parts) > 1 {
			name = parts[0] + "/" + parts[1]
		} else if len(parts) > 0 {
			name = parts[0]
		}
		return "dependency_missing", name, "npm"
	}
	if m := reNpmNotFound.FindStringSubmatch(stderr); len(m) == 2 {
		return "dependency_missing", m[1], "npm"
	}
	return "", "", ""
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
