package controller

import (
	"context"
	"sync"
	"testing"

	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/progress"
)

type fakeEngine struct{}

func (fakeEngine) GetStrategyName() string { return "fake" }

func (fakeEngine) Start(ctx context.Context, _ domain.StartRunRequest) (*domain.Run, error) {
	progress.Emit(ctx, progress.Event{Kind: progress.KindRun, Phase: progress.PhaseStart})
	return &domain.Run{ID: "child-run", Status: domain.RunStatusCompleted, FinalAnswer: "child answer"}, nil
}

type recordingHookDispatcher struct {
	events     []hookmodel.EventName
	blockStart bool
}

func (d *recordingHookDispatcher) Dispatch(_ context.Context, event hookmodel.Event) (hookmodel.AggregateResult, error) {
	d.events = append(d.events, event.Name)
	if d.blockStart && event.Name == hookmodel.EventSubagentStart {
		return hookmodel.AggregateResult{Blocked: true, BlockReason: "blocked by test"}, nil
	}
	return hookmodel.AggregateResult{}, nil
}

func TestControllerDispatchesSubagentLifecycleHooks(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(fakeEngine{}, limiter, nil)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &recordingHookDispatcher{}
	ctx := hookcontract.WithDispatcher(context.Background(), dispatcher)
	instance, err := c.Spawn(ctx, contract.SpawnRequest{SessionID: "session", ParentRunID: "parent", SubagentType: "explore", Prompt: "inspect", Agent: &domain.Agent{Name: "explore"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Wait(ctx, instance.AgentID); err != nil {
		t.Fatal(err)
	}
	if len(dispatcher.events) != 2 || dispatcher.events[0] != hookmodel.EventSubagentStart || dispatcher.events[1] != hookmodel.EventSubagentStop {
		t.Fatalf("unexpected lifecycle events: %+v", dispatcher.events)
	}
}

func TestControllerSubagentStartHookBlocksBeforeSlotReservation(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(fakeEngine{}, limiter, nil)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &recordingHookDispatcher{blockStart: true}
	ctx := hookcontract.WithDispatcher(context.Background(), dispatcher)
	if _, err := c.Spawn(ctx, contract.SpawnRequest{SessionID: "session", SubagentType: "explore", Prompt: "inspect", Agent: &domain.Agent{Name: "explore"}}); err == nil {
		t.Fatal("expected Hook to block spawn")
	}
	if _, err := limiter.Reserve(context.Background(), "session", 0); err != nil {
		t.Fatalf("slot should not be reserved when Hook blocks: %v", err)
	}
}

type panicEngine struct{ fakeEngine }

func (panicEngine) Start(context.Context, domain.StartRunRequest) (*domain.Run, error) { panic("boom") }

func TestControllerEmitsOnlyParentSubAgentProgress(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(fakeEngine{}, limiter, nil)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []progress.Event
	ctx := progress.WithSink(context.Background(), func(event progress.Event) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})
	instance, err := c.Spawn(ctx, contract.SpawnRequest{SessionID: "session", ParentRunID: "parent-run", Prompt: "inspect", Agent: &domain.Agent{Name: "explore"}})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := c.Wait(ctx, instance.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Summary != "child answer" || completed.ChildRunID != "child-run" {
		t.Fatalf("unexpected instance: %+v", completed)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected two parent events, got %+v", events)
	}
	for _, event := range events {
		if event.Kind != progress.KindSubAgent || event.RunID != "parent-run" {
			t.Fatalf("child event leaked into parent timeline: %+v", event)
		}
	}
}

func TestMemorySlotLimiterRejectsConcurrentSpawn(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := limiter.Reserve(context.Background(), "session", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limiter.Reserve(context.Background(), "session", 0); err == nil {
		t.Fatal("expected concurrent limit error")
	}
	if err := limiter.Release(first); err != nil {
		t.Fatal(err)
	}
}

func TestControllerReleasesSlotAfterEnginePanic(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(panicEngine{}, limiter, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Spawn(context.Background(), contract.SpawnRequest{SessionID: "session", Prompt: "first", Agent: &domain.Agent{Name: "first"}})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := c.Wait(context.Background(), first.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "failed" {
		t.Fatalf("expected failed instance, got %+v", completed)
	}
	if _, err := limiter.Reserve(context.Background(), "session", 0); err != nil {
		t.Fatalf("slot leaked after panic: %v", err)
	}
}
