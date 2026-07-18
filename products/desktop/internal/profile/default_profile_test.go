package profile

import "testing"

func TestDefaultProfileMatchesCurrentDesktopAssembly(t *testing.T) {
	prof := DefaultProfile(false)
	tools := toolSet(prof.Tools.Enabled)
	for _, required := range []string{"current_time", "calculator", "http_request", "todo_read", "todo_write", "todo_update_step", "Task", "TaskOutput", "TaskStop"} {
		if _, ok := tools[required]; !ok {
			t.Fatalf("Desktop 缺少已装配工具 %q", required)
		}
	}
	for _, ghost := range []string{"read_file", "write_file", "run_command", "Skill", "list_mcp_resources", "search_mcp_tools"} {
		if _, ok := tools[ghost]; ok {
			t.Fatalf("Desktop 不应声明未装配工具 %q", ghost)
		}
	}
	if prof.Skills.AllowImplicit {
		t.Fatal("Desktop 尚未装配 Skill 栈，不应允许隐式 Skill")
	}
}

func toolSet(names []string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		out[name] = struct{}{}
	}
	return out
}
