package gateway

import (
	"context"
	"testing"

	auditmemory "genesis-agent/internal/capabilities/audit/adapter/memory"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
	usagememory "genesis-agent/internal/capabilities/usage/adapter/memory"
)

type recordingAudit struct{ events []AuditEvent }

func (a *recordingAudit) RecordToolEvent(ctx context.Context, event AuditEvent) {
	a.events = append(a.events, event)
}

func TestGatewayHidesHiddenTools(t *testing.T) {
	registry := newFakeRegistry()
	registry.Register(fakeTool{name: "secret", traits: tool.ToolTraits{Exposure: tool.ToolExposureHidden}})
	g := New(registry, profilemodel.ToolSet{Enabled: []string{"*"}})
	if got := g.Get("secret"); got != nil {
		t.Fatal("hidden tool should not be executable")
	}
	if _, err := g.Execute(context.Background(), "secret", `{}`); err == nil {
		t.Fatal("hidden tool execution should fail")
	}
	for _, info := range g.ListInfos() {
		if info.Name == "secret" {
			t.Fatal("hidden tool should not be listed")
		}
	}
}

func TestGatewayRecordsAuditEvents(t *testing.T) {
	audit := &recordingAudit{}
	g := New(newFakeRegistry(), profilemodel.ToolSet{Enabled: []string{"calculator"}}, Options{Audit: audit})
	if _, err := g.Execute(context.Background(), "calculator", `{}`); err != nil {
		t.Fatal(err)
	}
	if len(audit.events) != 2 {
		t.Fatalf("events = %+v, want start and finish", audit.events)
	}
	if audit.events[0].Phase != "start" || audit.events[1].Phase != "finish" {
		t.Fatalf("events = %+v", audit.events)
	}
	if !audit.events[0].Traits.ReadOnly || !audit.events[0].Traits.ConcurrencySafe {
		t.Fatalf("calculator default traits = %+v", audit.events[0].Traits)
	}
}

type denyingAuthorizer struct{}

func (denyingAuthorizer) AuthorizeTool(ctx context.Context, request AuthorizationRequest) (AuthorizationDecision, error) {
	return AuthorizationDecision{Allowed: false, Reason: "需要确认"}, nil
}

type countingTool struct{ calls int }

func (t *countingTool) GetInfo() *tool.Info {
	return &tool.Info{Name: "write_file", Traits: tool.DefaultTraits("write_file")}
}

func (t *countingTool) Execute(context.Context, string) (string, error) {
	t.calls++
	return "ok", nil
}

func TestGatewayAuthorizerDeniesBeforeExecute(t *testing.T) {
	registry := newFakeRegistry()
	writeTool := &countingTool{}
	registry.Register(writeTool)
	audit := &recordingAudit{}
	g := New(registry, profilemodel.ToolSet{Enabled: []string{"write_file"}}, Options{Audit: audit, Authorizer: denyingAuthorizer{}})
	if _, err := g.Execute(context.Background(), "write_file", `{"path":"a.txt"}`); err == nil {
		t.Fatal("Execute(write_file) error = nil, want denied error")
	}
	if writeTool.calls != 0 {
		t.Fatalf("write_file executed %d times, want 0", writeTool.calls)
	}
	if len(audit.events) != 2 || audit.events[1].Allowed {
		t.Fatalf("audit events = %+v, want denied finish event", audit.events)
	}
}

func TestGatewayRecordsSystemAuditAndUsage(t *testing.T) {
	auditSink := auditmemory.NewSink()
	usageSink := usagememory.NewSink()
	g := New(newFakeRegistry(), profilemodel.ToolSet{Enabled: []string{"calculator"}}, Options{AuditSink: auditSink, UsageSink: usageSink})
	if _, err := g.Execute(context.Background(), "calculator", `{}`); err != nil {
		t.Fatal(err)
	}
	audits := auditSink.Events()
	if len(audits) != 2 || audits[0].Action != "calculator.start" || audits[1].Action != "calculator.finish" {
		t.Fatalf("audit events = %+v", audits)
	}
	usages := usageSink.ToolUsages()
	if len(usages) != 1 || usages[0].ToolName != "calculator" || !usages[0].Success || !usages[0].ReadOnly {
		t.Fatalf("usage events = %+v", usages)
	}
}
