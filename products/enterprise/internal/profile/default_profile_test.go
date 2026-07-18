package profile

import "testing"

func TestDefaultProfileExposesPlanToolsAndConditionalMCP(t *testing.T) {
	withoutMCP := DefaultProfile(false)
	set := make(map[string]struct{}, len(withoutMCP.Tools.Enabled))
	for _, name := range withoutMCP.Tools.Enabled {
		set[name] = struct{}{}
	}
	for _, required := range []string{"todo_read", "todo_write", "todo_update_step"} {
		if _, ok := set[required]; !ok {
			t.Fatalf("Enterprise 缺少计划工具 %q", required)
		}
	}
	if _, ok := set["search_mcp_tools"]; ok {
		t.Fatal("MCP 禁用时不应声明 search_mcp_tools")
	}

	withMCP := DefaultProfile(true)
	set = make(map[string]struct{}, len(withMCP.Tools.Enabled))
	for _, name := range withMCP.Tools.Enabled {
		set[name] = struct{}{}
	}
	if _, ok := set["search_mcp_tools"]; !ok {
		t.Fatal("MCP 启用时应声明 search_mcp_tools")
	}
}
