// Package policy 构建命令执行风险上下文。
package policy

import (
	"fmt"
	"strings"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
)

// Classification 描述命令风险分类。
type Classification struct {
	ReadOnly    bool
	Dangerous   bool
	Destructive bool
	Critical    bool
	Reason      string
}

// Classify 对命令做保守风险分类。
func Classify(command string) Classification {
	cmd := strings.ToLower(strings.TrimSpace(command))
	if cmd == "" {
		return Classification{Critical: true, Reason: "empty command"}
	}
	criticalTokens := []string{"format ", "mkfs", "shutdown", "reboot", "bcdedit", "diskpart"}
	for _, token := range criticalTokens {
		if strings.Contains(cmd, token) {
			return Classification{Critical: true, Destructive: true, Reason: "critical system command"}
		}
	}
	destructiveTokens := []string{"rm ", "rm -", "del ", "erase ", "rmdir", "remove-item", "git reset", "git clean", "chmod -r", "chown -r"}
	for _, token := range destructiveTokens {
		if strings.Contains(cmd, token) {
			return Classification{Dangerous: true, Destructive: true, Reason: "destructive command"}
		}
	}
	if containsShellControl(cmd) {
		return Classification{Dangerous: true, Reason: "compound shell command requires approval"}
	}
	readOnlyPrefixes := []string{"pwd", "ls", "dir", "cat ", "type ", "echo ", "git status", "git diff", "go version", "node --version", "npm --version"}
	for _, prefix := range readOnlyPrefixes {
		if cmd == strings.TrimSpace(prefix) || strings.HasPrefix(cmd, prefix) {
			return Classification{ReadOnly: true, Reason: "read-only command"}
		}
	}
	return Classification{Dangerous: true, Reason: "command execution requires approval"}
}

// BuildApprovalRequest 将命令上下文转换成通用审批请求。
func BuildApprovalRequest(toolName string, cmd execmodel.Command, cwd fsmodel.ResolvedPath, cls Classification) approvalmodel.Request {
	shell := cmd.Shell
	if shell == "" {
		shell = execmodel.ShellAuto
	}
	metadata := map[string]string{
		"scope":      string(cwd.Scope),
		"cwd":        cwd.DisplayPath,
		"backend":    cwd.BackendPath,
		"raw_path":   cwd.RawPath,
		"read_only":  fmt.Sprintf("%t", cls.ReadOnly),
		"resource":   "command",
		"path_scope": string(cwd.Scope),
		"shell":      string(shell),
	}
	if cls.Dangerous {
		metadata["dangerous"] = "true"
	}
	if cls.Destructive {
		metadata["destructive"] = "true"
	}
	if cls.Critical || cwd.Scope == fsmodel.PathScopeProtected {
		metadata["critical"] = "true"
		metadata["deny_reason"] = firstNonEmpty(cls.Reason, "critical command")
	}
	risk := approvalmodel.RiskMedium
	if cls.Dangerous || cwd.Scope == fsmodel.PathScopeExternal {
		risk = approvalmodel.RiskHigh
	}
	if metadata["critical"] == "true" {
		risk = approvalmodel.RiskCritical
	}
	return approvalmodel.Request{
		ToolName: toolName,
		Action:   approvalmodel.ActionCommandExec,
		Resource: approvalmodel.Resource{Type: "command", URI: "command://" + string(shell) + "/" + cmd.Command, Display: cmd.Command, Metadata: metadata},
		Reason:   firstNonEmpty(cls.Reason, "command execution requires approval"),
		Risk:     risk,
		SuggestedScopes: []approvalmodel.GrantScope{
			approvalmodel.GrantScopeOnce,
		},
		Metadata: metadata,
	}
}

func containsShellControl(cmd string) bool {
	controls := []string{"&&", "||", ";", "|", ">", "<", "`", "$("}
	for _, token := range controls {
		if strings.Contains(cmd, token) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
