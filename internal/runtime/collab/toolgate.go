package collab

import "strings"

// 规划模式允许的工具名（与 Profile Enabled 取交）。
// 未来通用问答工具（AskUserQuestion 类）可加入此表，勿塞进 planmode 包。
var planModeAllowlist = map[string]struct{}{
	"read_file":              {},
	"list_dir":               {},
	"walk_dir":               {},
	"glob":                   {},
	"grep":                   {},
	"current_time":           {},
	"calculator":             {},
	"web_search":             {},
	"web_fetch":              {},
	"Skill":                  {},
	"list_skill_resources":   {},
	"read_skill_resource":    {},
	"search_skill_resources": {},
	"Task":                   {},
	"TaskOutput":             {},
	"TaskStop":               {},
	"write_implementation_plan": {},
	"exit_plan_mode":            {},
}

// 执行中隐藏的规划专用工具。
var defaultModeHidden = map[string]struct{}{
	"write_implementation_plan": {},
	"exit_plan_mode":            {},
}

// FilterToolNames 按协作模式与委派深度过滤工具名。
// depth>0 表示子智能体：禁止 enter/exit 规划模式。
func FilterToolNames(mode Mode, depth int, names []string) []string {
	mode = Normalize(mode)
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !toolAllowed(mode, depth, name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// ToolAllowed 判断工具在当前模式/深度下是否允许执行（Gateway Authorizer 双保险）。
func ToolAllowed(mode Mode, depth int, toolName string) bool {
	return toolAllowed(Normalize(mode), depth, strings.TrimSpace(toolName))
}

func toolAllowed(mode Mode, depth int, name string) bool {
	if name == "" {
		return false
	}
	// 子智能体永远不能进出规划模式
	if depth > 0 {
		switch name {
		case "enter_plan_mode", "exit_plan_mode":
			return false
		}
	}
	if mode == ModePlan {
		if name == "enter_plan_mode" {
			return false
		}
		_, ok := planModeAllowlist[name]
		return ok
	}
	// default：隐藏规划专用写/退出工具
	if _, hidden := defaultModeHidden[name]; hidden {
		return false
	}
	return true
}

// PlanDocumentRelPath 返回会话实施方案的工作区相对路径。
func PlanDocumentRelPath(sessionID string) string {
	safe := sanitizeSessionID(sessionID)
	if safe == "" {
		safe = "session"
	}
	return ".genesis/plans/" + safe + ".md"
}

func sanitizeSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range sessionID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
