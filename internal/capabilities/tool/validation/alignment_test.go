package validation

import (
	"context"
	"testing"

	"genesis-agent/internal/capabilities/tool/adapter/registry"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

type fakeTool struct{ name string }

func (f fakeTool) GetInfo() *tool.Info                             { return &tool.Info{Name: f.name} }
func (f fakeTool) Execute(context.Context, string) (string, error) { return "", nil }

func TestValidateEnabled(t *testing.T) {
	reg := registry.NewRegistry()
	if err := reg.Register(fakeTool{name: "ready"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEnabled(reg, []string{"ready", "mcp__*"}); err != nil {
		t.Fatalf("有效声明不应失败: %v", err)
	}
	if err := ValidateEnabled(reg, []string{"ready", "ghost"}); err == nil {
		t.Fatal("幽灵工具应导致校验失败")
	}
}

func TestPromptToolsAvailable(t *testing.T) {
	reg := registry.NewRegistry()
	if err := reg.Register(fakeTool{name: "todo_write"}); err != nil {
		t.Fatal(err)
	}
	if !PromptToolsAvailable(reg, []string{"todo_write"}, []string{"todo_write"}) {
		t.Fatal("已启用且注册的提示词工具应可用")
	}
	if PromptToolsAvailable(reg, nil, []string{"todo_write"}) {
		t.Fatal("未启用工具不应进入提示词")
	}
}
