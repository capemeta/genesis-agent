package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/multiagent/contextsnapshot"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
	"genesis-agent/internal/runtime/multiagent/projection"
	"genesis-agent/internal/runtime/multiagent/result"
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

type failedRunEngine struct{ fakeEngine }

func (failedRunEngine) Start(context.Context, domain.StartRunRequest) (*domain.Run, error) {
	return &domain.Run{ID: "child-run", Status: domain.RunStatusFailed}, nil
}

type snapshotCheckingEngine struct{ fakeEngine }

func (snapshotCheckingEngine) Start(ctx context.Context, _ domain.StartRunRequest) (*domain.Run, error) {
	if _, err := (contextsnapshot.ContextSource{}).Snapshot(ctx); err == nil {
		return nil, fmt.Errorf("child Run inherited parent snapshot")
	}
	return &domain.Run{ID: "child-run", Status: domain.RunStatusCompleted, FinalAnswer: "done"}, nil
}

type artifactRegisteringEngine struct{ fakeEngine }

func (artifactRegisteringEngine) Start(ctx context.Context, _ domain.StartRunRequest) (*domain.Run, error) {
	if !result.RegisterArtifact(ctx, model.Artifact{ResourceID: "res-report", Kind: "file"}) {
		return nil, fmt.Errorf("artifact manifest registry missing")
	}
	if !result.RegisterFinding(ctx, model.Finding{Claim: "报告已生成", Evidence: []string{"res-report"}}) {
		return nil, fmt.Errorf("finding manifest registry missing")
	}
	return &domain.Run{ID: "child-run", Status: domain.RunStatusCompleted, FinalAnswer: "已生成报告"}, nil
}

type acceptingEvidenceValidator struct{}

func (acceptingEvidenceValidator) Validate(_ context.Context, manifest model.ArtifactManifest, findings []model.Finding) (result.ValidatedEvidence, error) {
	return result.ValidatedEvidence{Artifacts: append([]model.Artifact(nil), manifest.Artifacts...), Findings: append([]model.Finding(nil), findings...)}, nil
}

type passthroughResourceProjector struct{}

func (passthroughResourceProjector) ProjectArtifact(_ context.Context, artifact model.Artifact) (model.Artifact, bool, error) {
	return artifact, true, nil
}

type blockingEvidenceValidator struct {
	started chan struct{}
	release chan struct{}
}

func (v blockingEvidenceValidator) Validate(context.Context, model.ArtifactManifest, []model.Finding) (result.ValidatedEvidence, error) {
	close(v.started)
	<-v.release
	return result.ValidatedEvidence{}, nil
}

type waitCancelEngine struct{ fakeEngine }

func (waitCancelEngine) Start(ctx context.Context, _ domain.StartRunRequest) (*domain.Run, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type contextCheckingStore struct {
	delegate *memoryStore
	saves    int
}

func (s *contextCheckingStore) Save(ctx context.Context, value contract.StoredInstance) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.saves++
	return s.delegate.Save(ctx, value)
}

func (s *contextCheckingStore) Get(ctx context.Context, agentID string) (contract.StoredInstance, error) {
	return s.delegate.Get(ctx, agentID)
}

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
	if completed.Result == nil || completed.Result.Summary != "child answer" || completed.Result.ResultID == "" {
		t.Fatalf("expected structured safe result, got %+v", completed.Result)
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

func TestControllerEmitsProjectionEventsWithoutResultBody(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	sink := projection.NewMemorySink(model.ProjectionChannelDesktop)
	c, err := New(fakeEngine{}, limiter, nil, WithProjectionSink(sink))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := c.Spawn(context.Background(), contract.SpawnRequest{SessionID: "session", TenantID: "tenant", ParentRunID: "parent-run", SubagentType: "explore", Prompt: "inspect", Agent: &domain.Agent{Name: "explore"}})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := c.Wait(context.Background(), instance.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Result == nil {
		t.Fatal("expected terminal result")
	}
	events := sink.Events()
	if len(events) != 2 {
		t.Fatalf("expected spawned and completed projection events, got %+v", events)
	}
	if events[0].Type != model.ProjectionEventSpawned || events[0].Channel != model.ProjectionChannelDesktop || events[0].ResultID != "" {
		t.Fatalf("unexpected spawned event: %+v", events[0])
	}
	if events[1].Type != model.ProjectionEventCompleted || events[1].ResultID != completed.Result.ResultID {
		t.Fatalf("unexpected completed event: %+v", events[1])
	}
	for _, event := range events {
		for _, value := range event.Metadata {
			if strings.Contains(value, "child answer") {
				t.Fatalf("projection metadata leaked result body: %+v", event)
			}
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

func TestControllerAllowsDepthTwoAndRejectsDepthThree(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(2)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(fakeEngine{}, limiter, nil)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := c.Spawn(context.Background(), contract.SpawnRequest{SessionID: "session", Depth: 2, MaxDepth: 2, Prompt: "inspect", Agent: &domain.Agent{Name: "worker"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Wait(context.Background(), instance.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Spawn(context.Background(), contract.SpawnRequest{SessionID: "session", Depth: 3, MaxDepth: 2, Prompt: "inspect", Agent: &domain.Agent{Name: "worker"}}); err == nil {
		t.Fatal("expected depth three spawn to fail")
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

func TestControllerUsesChildRunTerminalStatus(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(failedRunEngine{}, limiter, nil)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := c.Spawn(context.Background(), contract.SpawnRequest{SessionID: "session", Prompt: "inspect", Agent: &domain.Agent{Name: "explore"}})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := c.Wait(context.Background(), instance.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.StatusFailed || completed.Result == nil || completed.Result.Status != model.ResultStatusFailed {
		t.Fatalf("child terminal status was not preserved: %+v", completed)
	}
}

func TestControllerDoesNotPassParentSnapshotToChildRun(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(snapshotCheckingEngine{}, limiter, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextsnapshot.WithParentSnapshot(context.Background(), []*domain.Message{domain.NewUserMessage("private")}, "call-1")
	instance, err := c.Spawn(ctx, contract.SpawnRequest{SessionID: "session", Prompt: "inspect", Agent: &domain.Agent{Name: "explore"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Wait(context.Background(), instance.AgentID); err != nil {
		t.Fatal(err)
	}
}

func TestControllerReducesRegisteredManifest(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(
		artifactRegisteringEngine{},
		limiter,
		nil,
		WithResultPipeline(
			result.Reducer{Evidence: acceptingEvidenceValidator{}},
			result.NewProjector(passthroughResourceProjector{}),
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := c.Spawn(context.Background(), contract.SpawnRequest{SessionID: "session", Prompt: "生成报告", Agent: &domain.Agent{Name: "report"}})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := c.Wait(context.Background(), instance.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Result == nil || len(completed.Result.Artifacts) != 1 || completed.Result.Artifacts[0].ResourceID != "res-report" {
		t.Fatalf("registered artifact was not reduced: %+v", completed.Result)
	}
	if len(completed.Result.Findings) != 1 || completed.Result.Findings[0].Claim != "报告已生成" {
		t.Fatalf("registered finding was not reduced: %+v", completed.Result)
	}
}

func TestControllerResumeUsesOnlyPriorSafeSummary(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(2)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(fakeEngine{}, limiter, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Spawn(context.Background(), contract.SpawnRequest{SessionID: "session", Prompt: "first", Agent: &domain.Agent{Name: "explore"}})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := c.Wait(context.Background(), first.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	completed.Result.Summary = "only safe summary"
	c.mu.Lock()
	c.instances[first.AgentID].instance = completed
	c.mu.Unlock()
	followup, err := c.Resume(context.Background(), first.AgentID, "verify next")
	if err != nil {
		t.Fatal(err)
	}
	c.mu.RLock()
	prompt := c.instances[followup.AgentID].request.Prompt
	c.mu.RUnlock()
	if !strings.Contains(prompt, "only safe summary") || !strings.Contains(prompt, "verify next") {
		t.Fatalf("resume prompt missing safe continuation: %q", prompt)
	}
}

func TestControllerGetAndResumeUseInjectedStoreAcrossControllers(t *testing.T) {
	store := newMemoryStore()
	limiter, err := NewMemorySlotLimiter(2)
	if err != nil {
		t.Fatal(err)
	}
	firstController, err := New(fakeEngine{}, limiter, nil, WithInstanceStore(store))
	if err != nil {
		t.Fatal(err)
	}
	first, err := firstController.Spawn(context.Background(), contract.SpawnRequest{SessionID: "session", TenantID: "tenant", ParentRunID: "parent", Prompt: "first", Agent: &domain.Agent{Name: "explore"}})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := firstController.Wait(context.Background(), first.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Result == nil {
		t.Fatal("expected stored terminal result")
	}

	nextController, err := New(fakeEngine{}, limiter, nil, WithInstanceStore(store))
	if err != nil {
		t.Fatal(err)
	}
	fromStore, err := nextController.Get(context.Background(), first.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if fromStore.Result == nil || fromStore.Result.Summary != "child answer" {
		t.Fatalf("stored result not returned: %+v", fromStore)
	}
	followup, err := nextController.Resume(context.Background(), first.AgentID, "verify next")
	if err != nil {
		t.Fatal(err)
	}
	if followup.AgentID == first.AgentID {
		t.Fatalf("resume must create a new unique instance id: %q", followup.AgentID)
	}
	nextController.mu.RLock()
	prompt := nextController.instances[followup.AgentID].request.Prompt
	nextController.mu.RUnlock()
	if !strings.Contains(prompt, "child answer") || !strings.Contains(prompt, "verify next") {
		t.Fatalf("resume prompt missing stored safe summary: %q", prompt)
	}
}

func TestControllerPersistsTerminalStateAfterParentCancel(t *testing.T) {
	store := &contextCheckingStore{delegate: newMemoryStore()}
	sink := projection.NewMemorySink(model.ProjectionChannelCLI)
	limiter, err := NewMemorySlotLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(waitCancelEngine{}, limiter, nil, WithInstanceStore(store), WithProjectionSink(sink))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	instance, err := c.Spawn(ctx, contract.SpawnRequest{SessionID: "session", TenantID: "tenant", ParentRunID: "parent", Prompt: "wait", Agent: &domain.Agent{Name: "wait"}})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	completed, err := c.Wait(context.Background(), instance.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.StatusCancelled || completed.Result == nil {
		t.Fatalf("expected cancelled terminal result, got %+v", completed)
	}
	stored, err := store.Get(context.Background(), instance.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Instance.Result == nil || stored.Instance.Status != model.StatusCancelled {
		t.Fatalf("terminal state was not persisted: %+v", stored.Instance)
	}
	if store.saves < 2 {
		t.Fatalf("expected initial and terminal saves, got %d", store.saves)
	}
	events := sink.Events()
	if len(events) != 2 || events[1].Type != model.ProjectionEventStopped {
		t.Fatalf("terminal projection should survive parent cancellation: %+v", events)
	}
}

func TestControllerDoesNotHoldInstanceLockDuringEvidenceValidation(t *testing.T) {
	limiter, err := NewMemorySlotLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	validator := blockingEvidenceValidator{started: make(chan struct{}), release: make(chan struct{})}
	c, err := New(artifactRegisteringEngine{}, limiter, nil, WithResultPipeline(result.Reducer{Evidence: validator}, result.NewProjector(passthroughResourceProjector{})))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := c.Spawn(context.Background(), contract.SpawnRequest{SessionID: "session", Prompt: "生成报告", Agent: &domain.Agent{Name: "report"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-validator.started:
	case <-time.After(time.Second):
		t.Fatal("evidence validation did not start")
	}
	getDone := make(chan error, 1)
	go func() {
		_, getErr := c.Get(context.Background(), instance.AgentID)
		getDone <- getErr
	}()
	select {
	case getErr := <-getDone:
		if getErr != nil {
			t.Fatal(getErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Get was blocked by evidence validation")
	}
	close(validator.release)
	if _, err := c.Wait(context.Background(), instance.AgentID); err != nil {
		t.Fatal(err)
	}
}
