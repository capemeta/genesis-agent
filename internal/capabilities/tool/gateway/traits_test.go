package gateway

import (
	"context"
	"sync"
	"testing"
	"time"

	auditmemory "genesis-agent/internal/capabilities/audit/adapter/memory"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
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

type blockingTaskTool struct {
	release <-chan struct{}
	active  chan struct{}
}

func (t *blockingTaskTool) GetInfo() *tool.Info {
	return tool.WithTraits(&tool.Info{Name: "Task"}, tool.ToolTraits{
		Exposure: tool.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: true, NeedsPermission: true,
	})
}

func (t *blockingTaskTool) Execute(context.Context, string) (string, error) {
	t.active <- struct{}{}
	<-t.release
	return `{"ok":true}`, nil
}

func TestGatewayAllowsParallelConcurrencySafeSameNameTools(t *testing.T) {
	release := make(chan struct{})
	active := make(chan struct{}, 2)
	shared := &blockingTaskTool{release: release, active: active}
	reg := &fakeRegistry{tools: map[string]tool.Tool{}}
	if err := reg.Register(shared); err != nil {
		t.Fatal(err)
	}
	g := New(reg, profilemodel.ToolSet{Enabled: []string{"Task"}}, Options{Locker: scheduler.NewMemoryResourceLocker()})

	var wg sync.WaitGroup
	wg.Add(2)
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := g.Execute(context.Background(), "Task", `{"prompt":"x"}`)
			errCh <- err
		}()
	}

	// 共享读锁下两个同名 ConcurrencySafe 调用应同时进入 Execute；若仍用写锁则第二个会阻塞。
	for i := 0; i < 2; i++ {
		select {
		case <-active:
		case <-time.After(time.Second):
			t.Fatalf("仅 %d 个 Task 进入 Execute，写锁可能误串行了 ConcurrencySafe 工具", i)
		}
	}
	close(release)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
	}
}
