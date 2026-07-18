package profile

import "testing"

func TestDefaultProfileExposesPlanAndInteractiveTools(t *testing.T) {
	prof := DefaultProfile(false)
	set := make(map[string]struct{}, len(prof.Tools.Enabled))
	for _, name := range prof.Tools.Enabled {
		set[name] = struct{}{}
	}
	for _, required := range []string{"todo_read", "todo_write", "todo_update_step", "write_stdin"} {
		if _, ok := set[required]; !ok {
			t.Fatalf("CLI 缺少已注册工具 %q", required)
		}
	}
	for _, mcpTool := range []string{"list_mcp_resources", "read_mcp_resource", "search_mcp_tools"} {
		if _, ok := set[mcpTool]; ok {
			t.Fatalf("MCP 禁用时不应声明 %q", mcpTool)
		}
	}
}
