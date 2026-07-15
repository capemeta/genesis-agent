package service

import (
	"context"
	"testing"

	auditmemory "genesis-agent/internal/capabilities/audit/adapter/memory"
	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	"genesis-agent/internal/capabilities/hook/model"
)

type testRunner struct{ decision model.Decision }

func (testRunner) Kind() string                                                    { return "builtin" }
func (r testRunner) Run(context.Context, model.HandlerSpec, []byte) model.Decision { return r.decision }

type commandTestRunner struct{ calls int }

func (*commandTestRunner) Kind() string { return "command" }
func (r *commandTestRunner) Run(context.Context, model.HandlerSpec, []byte) model.Decision {
	r.calls++
	return model.Decision{Continue: true}
}

func TestDispatcherAggregatesInConfigurationOrder(t *testing.T) {
	d := NewDispatcher(model.Config{Events: map[model.EventName][]model.HookSpec{
		model.EventPreToolUse: {{Matcher: "run_*", Handlers: []model.HandlerSpec{{Type: "builtin"}}}},
	}}, testRunner{decision: model.Decision{Continue: true, UpdatedInput: map[string]any{"command": "safe"}, AdditionalContext: "first"}})
	result, err := d.Dispatch(context.Background(), model.Event{Name: model.EventPreToolUse, MatchKey: "run_command"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Blocked || result.UpdatedInput["command"] != "safe" || len(result.AdditionalContext) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDispatcherBlocksDeny(t *testing.T) {
	d := NewDispatcher(model.Config{Events: map[model.EventName][]model.HookSpec{
		model.EventPreToolUse: {{Handlers: []model.HandlerSpec{{Type: "builtin"}}}},
	}}, testRunner{decision: model.Decision{Continue: true, PermissionDecision: "deny", Reason: "not allowed"}})
	result, err := d.Dispatch(context.Background(), model.Event{Name: model.EventPreToolUse})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Blocked || result.BlockReason != "not allowed" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestAdditionalContextQueue(t *testing.T) {
	ctx := hookcontract.WithDispatcher(context.Background(), NewDispatcher(model.Config{}))
	hookcontract.AppendAdditionalContext(ctx, "a", "b")
	if got := hookcontract.DrainAdditionalContext(ctx); len(got) != 2 || got[0] != "a" {
		t.Fatalf("unexpected: %#v", got)
	}
	if got := hookcontract.DrainAdditionalContext(ctx); len(got) != 0 {
		t.Fatalf("queue not drained: %#v", got)
	}
}

func TestDispatcherRequiresMatchingTrustedHashForCommand(t *testing.T) {
	runner := &commandTestRunner{}
	spec := model.HandlerSpec{Type: "command", Command: "echo guard"}
	d := NewDispatcher(model.Config{Events: map[model.EventName][]model.HookSpec{model.EventPreToolUse: {{Handlers: []model.HandlerSpec{spec}}}}}, runner)
	result, err := d.Dispatch(context.Background(), model.Event{Name: model.EventPreToolUse})
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 0 || len(result.Warnings) != 1 {
		t.Fatalf("untrusted command ran: calls=%d result=%#v", runner.calls, result)
	}

	spec.TrustedHash = HandlerFingerprint(spec)
	d = NewDispatcher(model.Config{Events: map[model.EventName][]model.HookSpec{model.EventPreToolUse: {{Handlers: []model.HandlerSpec{spec}}}}}, runner)
	if _, err := d.Dispatch(context.Background(), model.Event{Name: model.EventPreToolUse}); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 {
		t.Fatalf("trusted command calls=%d, want 1", runner.calls)
	}
}

func TestDispatcherAppliesScopeAndState(t *testing.T) {
	runner := &commandTestRunner{}
	spec := model.HandlerSpec{Type: "command", Command: "echo guard", TrustedHash: "sha256:bad"}
	group := model.HookSpec{Scope: model.Scope{TenantIDs: []string{"tenant-a"}}, Handlers: []model.HandlerSpec{spec}}
	d := NewDispatcher(model.Config{Events: map[model.EventName][]model.HookSpec{model.EventPreToolUse: {group}}}, runner)
	ctx := hookcontract.WithScopeContext(context.Background(), model.ScopeContext{TenantID: "tenant-b"})
	if result, err := d.Dispatch(ctx, model.Event{Name: model.EventPreToolUse}); err != nil || len(result.Warnings) != 0 {
		t.Fatalf("scope mismatch result=%#v err=%v", result, err)
	}
	if runner.calls != 0 {
		t.Fatal("scope mismatch ran command")
	}

	disabled := false
	key := HandlerKey(model.EventPreToolUse, group.Matcher, spec)
	d = NewDispatcher(model.Config{Events: map[model.EventName][]model.HookSpec{model.EventPreToolUse: {group}}, State: map[string]model.HookState{key: {Enabled: &disabled}}}, runner)
	ctx = hookcontract.WithScopeContext(context.Background(), model.ScopeContext{TenantID: "tenant-a"})
	if _, err := d.Dispatch(ctx, model.Event{Name: model.EventPreToolUse}); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 0 {
		t.Fatal("disabled handler ran")
	}
}

func TestDispatcherWritesAuditLifecycle(t *testing.T) {
	audit := auditmemory.NewSink()
	d := NewDispatcherWithOptions(model.Config{Events: map[model.EventName][]model.HookSpec{model.EventPreToolUse: {{Handlers: []model.HandlerSpec{{Type: "builtin"}}}}}}, []DispatcherOption{WithAuditSink(audit)}, testRunner{decision: model.Decision{Continue: true}})
	if _, err := d.Dispatch(context.Background(), model.Event{Name: model.EventPreToolUse}); err != nil {
		t.Fatal(err)
	}
	if events := audit.Events(); len(events) != 2 || events[0].Action != "PreToolUse.start" || events[1].Action != "PreToolUse.complete" {
		t.Fatalf("unexpected audit events: %#v", events)
	}
}

func TestDispatcherAuditsUntrustedCommand(t *testing.T) {
	audit := auditmemory.NewSink()
	d := NewDispatcherWithOptions(model.Config{Events: map[model.EventName][]model.HookSpec{model.EventPreToolUse: {{Handlers: []model.HandlerSpec{{Type: "command", Command: "echo guard"}}}}}}, []DispatcherOption{WithAuditSink(audit)}, &commandTestRunner{})
	if _, err := d.Dispatch(context.Background(), model.Event{Name: model.EventPreToolUse}); err != nil {
		t.Fatal(err)
	}
	if events := audit.Events(); len(events) != 1 || events[0].Action != "PreToolUse.rejected" || events[0].Allowed {
		t.Fatalf("unexpected audit events: %#v", events)
	}
}
