package registry

import (
	"context"
	"strings"
	"testing"

	tool "genesis-agent/internal/capabilities/tool/contract"
)

type testTool struct{ name string }

func (t *testTool) GetInfo() *tool.Info                             { return &tool.Info{Name: t.name} }
func (t *testTool) Execute(context.Context, string) (string, error) { return "ok", nil }

func TestRegisterRejectsDuplicateAndReplaceChecksOwner(t *testing.T) {
	r := NewRegistry()
	first := &testTool{name: "same"}
	if err := r.Register(first); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	if err := r.Register(&testTool{name: "same"}); err == nil || !strings.Contains(err.Error(), "拒绝") {
		t.Fatalf("重复注册应被拒绝: %v", err)
	}
	owner, ok := r.Owner("same")
	if !ok || owner == "" {
		t.Fatalf("注册 owner 缺失: owner=%q ok=%v", owner, ok)
	}
	if err := r.Replace("same", "wrong-owner", &testTool{name: "same"}); err == nil {
		t.Fatal("错误 expectedOwner 不应替换成功")
	}
	replacement := &testTool{name: "same"}
	if err := r.Replace("same", owner, replacement); err != nil {
		t.Fatalf("显式替换失败: %v", err)
	}
	if got := r.Get("same"); got != replacement {
		t.Fatalf("替换结果错误: %#v", got)
	}
}

func TestReplaceRejectsNameMismatch(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&testTool{name: "old"}); err != nil {
		t.Fatal(err)
	}
	owner, _ := r.Owner("old")
	if err := r.Replace("old", owner, &testTool{name: "new"}); err == nil {
		t.Fatal("名称不一致不应替换成功")
	}
}
