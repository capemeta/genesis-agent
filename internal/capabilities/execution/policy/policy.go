// Package policy 构建命令执行风险上下文。
package policy

import (
	"fmt"
	"runtime"
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
	shell := execmodel.ShellBash
	if runtime.GOOS == "windows" {
		shell = execmodel.ShellPowerShell
	}
	return ClassifyCommand(execmodel.Command{Command: command, Shell: shell})
}

// ClassifyCommand 按实际 Shell 语义对命令做保守风险分类。
// 无法可靠解析时必须要求审批，不能把未知命令误判为只读。
func ClassifyCommand(command execmodel.Command) Classification {
	cmd := strings.ToLower(strings.TrimSpace(command.Command))
	base := classifyCriticalOrDestructive(cmd)
	if base != nil {
		return *base
	}
	switch command.Shell {
	case execmodel.ShellPowerShell:
		return classifyPowerShell(cmd)
	case execmodel.ShellCmd:
		return classifyCmd(cmd)
	case execmodel.ShellBash, execmodel.ShellSh, execmodel.ShellZsh:
		return classifyPOSIX(cmd)
	default:
		return Classification{Dangerous: true, Reason: "unknown shell requires approval"}
	}
}

func classifyCriticalOrDestructive(cmd string) *Classification {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	if cmd == "" {
		result := Classification{Critical: true, Reason: "empty command"}
		return &result
	}
	criticalTokens := []string{"format ", "mkfs", "shutdown", "reboot", "bcdedit", "diskpart"}
	for _, token := range criticalTokens {
		if strings.Contains(cmd, token) {
			result := Classification{Critical: true, Destructive: true, Reason: "critical system command"}
			return &result
		}
	}
	destructiveTokens := []string{"rm ", "rm -", "del ", "erase ", "rmdir", "remove-item", "git reset", "git clean", "chmod -r", "chown -r"}
	for _, token := range destructiveTokens {
		if strings.Contains(cmd, token) {
			result := Classification{Dangerous: true, Destructive: true, Reason: "destructive command"}
			return &result
		}
	}
	return nil
}

func classifyPOSIX(cmd string) Classification {
	if containsShellControl(cmd) {
		return Classification{Dangerous: true, Reason: "compound shell command requires approval"}
	}
	readOnlyPrefixes := []string{"pwd", "ls", "dir", "cat ", "type ", "echo ", "git status", "git diff", "go version", "node --version", "npm --version"}
	for _, prefix := range readOnlyPrefixes {
		if matchesCommandPrefix(cmd, prefix) {
			return Classification{ReadOnly: true, Reason: "read-only command"}
		}
	}
	return Classification{Dangerous: true, Reason: "command execution requires approval"}
}

func classifyCmd(cmd string) Classification {
	if containsShellControl(cmd) || strings.ContainsAny(cmd, "%!") {
		return Classification{Dangerous: true, Reason: "compound cmd command requires approval"}
	}
	readOnlyPrefixes := []string{"cd", "chdir", "dir", "echo", "set", "type ", "where ", "findstr ", "git status", "git diff"}
	for _, prefix := range readOnlyPrefixes {
		if matchesCommandPrefix(cmd, prefix) {
			return Classification{ReadOnly: true, Reason: "read-only cmd command"}
		}
	}
	return Classification{Dangerous: true, Reason: "cmd command requires approval"}
}

func matchesCommandPrefix(command, prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	return command == prefix || strings.HasPrefix(command, prefix+" ") || strings.HasPrefix(command, prefix+"\t")
}

func classifyPowerShell(cmd string) Classification {
	if strings.ContainsAny(cmd, ">;`&\r\n") || strings.Contains(cmd, "$(") || strings.Contains(cmd, "||") {
		return Classification{Dangerous: true, Reason: "compound PowerShell command requires approval"}
	}
	segments := strings.Split(cmd, "|")
	for _, segment := range segments {
		fields := strings.Fields(strings.TrimSpace(segment))
		if len(fields) == 0 || !isReadOnlyPowerShellCommand(fields) {
			return Classification{Dangerous: true, Reason: "PowerShell command requires approval"}
		}
	}
	return Classification{ReadOnly: true, Reason: "read-only PowerShell command"}
}

func firstCommandWord(segment string) string {
	fields := strings.Fields(strings.TrimSpace(segment))
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(strings.ToLower(fields[0]), "&()")
}

func isReadOnlyPowerShellCommand(fields []string) bool {
	command := strings.Trim(strings.ToLower(fields[0]), "&()")
	switch command {
	case "echo", "write-output", "write-host", "dir", "ls", "get-childitem", "gci",
		"cat", "type", "gc", "get-content", "select-string", "sls", "findstr",
		"measure-object", "measure", "get-location", "gl", "pwd", "test-path", "tp",
		"resolve-path", "rvpa", "select-object", "select", "get-item", "get-process", "rg":
		return true
	case "git":
		return isReadOnlyGit(fields[1:])
	default:
		return false
	}
}

func isReadOnlyGit(args []string) bool {
	for _, arg := range args {
		arg = strings.ToLower(strings.TrimSpace(arg))
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		switch arg {
		case "status", "diff", "log", "show", "rev-parse", "ls-files", "grep":
			return true
		default:
			return false
		}
	}
	return false
}

// RecoveryAdvice 是失败后提供给模型的结构化换路建议。
type RecoveryAdvice struct {
	Action               string
	Tool                 string
	Reason               string
	OperationFingerprint string
}

// RecoveryHint 识别本应由结构化工具完成的常见 Shell 操作。
func RecoveryHint(command execmodel.Command) *RecoveryAdvice {
	lower := strings.ToLower(strings.TrimSpace(command.Command))
	first := firstCommandWord(lower)
	if isShellPathListingSearch(first, lower) {
		return &RecoveryAdvice{
			Action:               "use_glob_for_path_pattern",
			Tool:                 "glob",
			Reason:               "路径发现应使用结构化 glob 工具，避免 Shell 路径枚举差异与非零退出误导",
			OperationFingerprint: "filesystem.glob",
		}
	}
	if isShellContentSearch(first, lower) {
		return &RecoveryAdvice{
			Action:               "use_grep_for_content_search",
			Tool:                 "grep",
			Reason:               "workspace 文本搜索应使用结构化 grep 工具，避免 Shell 搜索差异与退出码语义干扰",
			OperationFingerprint: "filesystem.search",
		}
	}
	if isShellPathEnumeration(first, lower) {
		if hasPathGlobOperand(lower) {
			return &RecoveryAdvice{
				Action:               "use_glob_for_path_pattern",
				Tool:                 "glob",
				Reason:               "通配路径查找应使用结构化 glob 工具，避免 Shell 通配差异与非零退出误导",
				OperationFingerprint: "filesystem.glob",
			}
		}
		return &RecoveryAdvice{
			Action:               "use_list_dir_for_directory_enumeration",
			Tool:                 "list_dir",
			Reason:               "目录枚举应使用结构化文件工具，避免Shell差异、转义和额外沙箱开销",
			OperationFingerprint: "filesystem.list",
		}
	}
	return nil
}

func isShellPathEnumeration(first, lower string) bool {
	switch first {
	case "ls", "dir", "get-childitem", "gci":
		return true
	}
	if (strings.HasPrefix(first, "powershell") || strings.HasPrefix(first, "pwsh")) && strings.Contains(lower, "get-childitem") {
		return true
	}
	if strings.HasPrefix(first, "cmd") && strings.Contains(lower, " dir ") {
		return true
	}
	return false
}

func isShellContentSearch(first, lower string) bool {
	switch first {
	case "grep", "findstr", "select-string", "sls":
		return true
	case "rg":
		// rg --files / -l 等偏路径发现，不应导向内容 grep。
		return !isRipgrepPathListing(lower)
	}
	// 管道中的 grep 常用于二次过滤抽取结果，不能简单改写成 filesystem grep（例如 markitdown|grep）。
	return false
}

func isShellPathListingSearch(first, lower string) bool {
	return first == "rg" && isRipgrepPathListing(lower)
}

func isRipgrepPathListing(lower string) bool {
	fields := strings.Fields(lower)
	for _, f := range fields[1:] {
		// 仅纯路径枚举；-l/--files-with-matches 仍是内容搜索（只是输出文件名），应走 grep。
		switch f {
		case "--files":
			return true
		}
		if strings.HasPrefix(f, "--files=") {
			return true
		}
	}
	return false
}

// hasPathGlobOperand 仅在位置参数/路径操作数含通配时返回 true。
// 避免把 ls -I '*.o' 这类选项过滤误判为 path glob；也不把 -la 误当成吞参选项。
func hasPathGlobOperand(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	for i := 1; i < len(fields); i++ {
		f := fields[i]
		if strings.HasPrefix(f, "-") {
			if optionTakesSeparateValue(f) && i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "-") {
				i++
			}
			continue
		}
		if strings.ContainsAny(f, "*?[") {
			return true
		}
	}
	return false
}

func optionTakesSeparateValue(flag string) bool {
	if strings.HasPrefix(flag, "--") {
		name := strings.TrimPrefix(flag, "--")
		if strings.Contains(name, "=") {
			return false
		}
		switch name {
		case "ignore", "exclude", "include", "hide", "block-size":
			return true
		default:
			return false
		}
	}
	// 仅识别单字母且明确吞下一参数的短选项（如 ls -I）。
	// RecoveryHint 入参可能已被 ToLower，故同时接受 i/I。
	if len(flag) == 2 && flag[0] == '-' {
		return flag[1] == 'i' || flag[1] == 'I'
	}
	return false
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
	controls := []string{"&", "||", ";", "|", ">", "<", "`", "$(", "\r", "\n"}
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
