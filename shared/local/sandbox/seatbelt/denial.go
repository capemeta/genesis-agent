package seatbelt

import (
	"strings"
)

// Violation 描述一次 macOS Seatbelt 沙箱拒绝事件。
type Violation struct {
	// Operation 被拒绝的操作类型（如 "file-read*"、"network-outbound"）。
	Operation string
	// Path 被拒绝的路径（如有）。
	Path string
	// Raw 原始违规文本行。
	Raw string
}

// ParseDenials 从进程 stderr 输出中提取 Seatbelt 拒绝事件。
// Genesis sandbox profile 使用 (with message "GENESIS_SANDBOX") 标记所有 deny 规则，
// macOS 会将拒绝信息以 "sandbox-exec: GENESIS_SANDBOX: operation denied" 格式写入 stderr。
// 此函数识别这些行并结构化返回，供 runner 填写 result.SandboxViolations。
func ParseDenials(stderr string) []Violation {
	if stderr == "" {
		return nil
	}
	var violations []Violation
	for _, line := range strings.Split(stderr, "\n") {
		v, ok := parseDenialLine(line)
		if ok {
			violations = append(violations, v)
		}
	}
	return violations
}

// HasDenial 快速判断 stderr 中是否含 Seatbelt 拒绝标记。
func HasDenial(stderr string) bool {
	return strings.Contains(stderr, sandboxMarker)
}

// sandboxMarker 是 profile 中所有 deny 规则携带的 with-message 标记。
const sandboxMarker = "GENESIS_SANDBOX"

// parseDenialLine 尝试解析单行 sandbox 拒绝信息。
// macOS sandbox-exec 通常以以下格式输出拒绝行：
//
//	sandbox-exec: <msg>: <operation> denied
//	sandbox: <operation>(<args>) deny.<reason>
//
// 具体格式在不同 macOS 版本有差异，此处以 GENESIS_SANDBOX 标记为识别依据。
func parseDenialLine(line string) (Violation, bool) {
	line = strings.TrimSpace(line)
	if line == "" || !strings.Contains(line, sandboxMarker) {
		return Violation{}, false
	}

	v := Violation{Raw: line}

	// 尝试提取 operation：取 "deny" 前的 token
	lower := strings.ToLower(line)
	if idx := strings.Index(lower, "deny"); idx > 0 {
		// 取 deny 前最后一个非空 token 作为 operation
		before := strings.TrimSpace(line[:idx])
		if parts := strings.Fields(before); len(parts) > 0 {
			last := parts[len(parts)-1]
			// 去掉可能的括号、冒号等标点
			last = strings.TrimRight(last, ":,()")
			if last != "" {
				v.Operation = last
			}
		}
		// 取 deny 后内容尝试提取路径
		after := strings.TrimSpace(line[idx+4:])
		after = strings.TrimLeft(after, " .")
		// 若 after 以 "/" 开头，视为路径
		if strings.HasPrefix(after, "/") {
			// 路径可能后跟空格或换行，取首个 token
			if parts := strings.Fields(after); len(parts) > 0 {
				v.Path = parts[0]
			}
		}
	}

	return v, true
}

// FormatWarnings 从 stderr 中提取 Seatbelt 拒绝事件并格式化为友好的提示信息。
func FormatWarnings(stderr string) []string {
	violations := ParseDenials(stderr)
	if len(violations) == 0 {
		return nil
	}
	warnings := make([]string, 0, len(violations))
	for _, v := range violations {
		if v.Path != "" && v.Operation != "" {
			warnings = append(warnings, "试图访问 "+v.Path+" (操作: "+v.Operation+")，已触发 macOS Seatbelt 拦截")
		} else if v.Path != "" {
			warnings = append(warnings, "试图访问 "+v.Path+"，已触发 macOS Seatbelt 拦截")
		} else if v.Operation != "" {
			warnings = append(warnings, "试图执行 "+v.Operation+" 操作，已触发 macOS Seatbelt 拦截")
		} else {
			warnings = append(warnings, "操作已触发 macOS Seatbelt 拦截: "+v.Raw)
		}
	}
	return warnings
}
