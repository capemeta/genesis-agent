package searchmcptools

import (
	"context"
	"testing"

	"genesis-agent/internal/capabilities/mcp/model"
	"genesis-agent/internal/capabilities/mcp/tooladapter"
	"genesis-agent/internal/capabilities/tool/adapter/registry"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

func TestPromoteUpdatesMCPToolExposureThroughUpdater(t *testing.T) {
	reg := registry.NewRegistry()
	deferred := tooladapter.New(nil, "files", "search", "mcp__files__search", model.ToolSnapshot{Name: "search"}, tool.ToolExposureDeferred, 0)
	if err := reg.Register(deferred); err != nil {
		t.Fatal(err)
	}

	result, err := New(reg).Execute(context.Background(), `{"promote":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if result == "[]" {
		t.Fatal("应返回已提升的 MCP tool")
	}
	if got := tool.TraitsOf(reg.Get("mcp__files__search").GetInfo()).Exposure; got != tool.ToolExposureDirect {
		t.Fatalf("提升后 exposure=%q，期望 direct", got)
	}
}

func TestRejectsUnknownField(t *testing.T) {
	reg := registry.NewRegistry()
	if _, err := New(reg).Execute(context.Background(), `{"unknown":true}`); err == nil {
		t.Fatal("未知字段应被拒绝")
	}
}

func TestMCPToolInfoIsSnapshot(t *testing.T) {
	mcpTool := tooladapter.New(nil, "files", "search", "mcp__files__search", model.ToolSnapshot{Name: "search"}, tool.ToolExposureDeferred, 0)
	info := mcpTool.GetInfo()
	info.Traits.Exposure = tool.ToolExposureDirect

	if got := tool.TraitsOf(mcpTool.GetInfo()).Exposure; got != tool.ToolExposureDeferred {
		t.Fatalf("修改 GetInfo 返回值不应改变内部 exposure，实际=%q", got)
	}
}
