package service

import (
	"testing"

	"genesis-agent/internal/capabilities/hook/model"
)

func TestListEffectiveHandlersHonorsScopeAndTrust(t *testing.T) {
	enabled := true
	disabled := false
	command := model.HandlerSpec{Name: "project-command", Type: "command", Command: "echo ok"}
	config := model.Config{Enabled: &enabled, Events: map[model.EventName][]model.HookSpec{
		model.EventPreToolUse: {
			{Matcher: "read_file", Scope: model.Scope{Channels: []string{"cli"}}, Handlers: []model.HandlerSpec{{Name: "builtin", Type: "builtin", Builtin: "secret_path_guard"}, command}},
			{Matcher: "*", Enabled: &disabled, Handlers: []model.HandlerSpec{{Name: "disabled", Type: "builtin", Builtin: "git_branch_guard"}}},
			{Matcher: "*", Scope: model.Scope{Channels: []string{"enterprise"}}, Handlers: []model.HandlerSpec{{Name: "enterprise-only", Type: "builtin", Builtin: "git_branch_guard"}}},
		},
	}}
	items := ListEffectiveHandlers(config, model.ScopeContext{Channel: "cli"})
	if len(items) != 2 {
		t.Fatalf("expected two CLI handlers, got %+v", items)
	}
	if items[0].Name != "builtin" || !items[0].Trusted {
		t.Fatalf("unexpected builtin item: %+v", items[0])
	}
	if items[1].Name != "project-command" || items[1].Trusted {
		t.Fatalf("expected untrusted command item: %+v", items[1])
	}
}
