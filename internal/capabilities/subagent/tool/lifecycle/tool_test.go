package lifecycle

import (
	"context"
	"encoding/json"
	"testing"

	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

type fakeController struct {
	instance model.Instance
	stopped  bool
}

func (f *fakeController) Spawn(context.Context, contract.SpawnRequest) (model.Instance, error) {
	return f.instance, nil
}
func (f *fakeController) Wait(context.Context, string) (model.Instance, error) {
	return f.instance, nil
}
func (f *fakeController) Resume(context.Context, string, string) (model.Instance, error) {
	return f.instance, nil
}
func (f *fakeController) Stop(context.Context, string) error                  { f.stopped = true; return nil }
func (f *fakeController) Get(context.Context, string) (model.Instance, error) { return f.instance, nil }

type sequenceController struct {
	values []model.Instance
	index  int
}

type recordingCancellationStore struct {
	agentID string
	runID   string
}

func (s *recordingCancellationStore) RequestStop(_ context.Context, agentID, requesterRunID string) error {
	s.agentID = agentID
	s.runID = requesterRunID
	return nil
}
func (s *recordingCancellationStore) PollStop(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *recordingCancellationStore) ClearStop(context.Context, string, string) error {
	return nil
}

func (f *sequenceController) Spawn(context.Context, contract.SpawnRequest) (model.Instance, error) {
	return f.Get(context.Background(), "")
}
func (f *sequenceController) Wait(context.Context, string) (model.Instance, error) {
	return f.Get(context.Background(), "")
}
func (f *sequenceController) Resume(context.Context, string, string) (model.Instance, error) {
	return f.Get(context.Background(), "")
}
func (f *sequenceController) Stop(context.Context, string) error { return nil }
func (f *sequenceController) Get(context.Context, string) (model.Instance, error) {
	if f.index >= len(f.values) {
		return f.values[len(f.values)-1], nil
	}
	value := f.values[f.index]
	f.index++
	return value, nil
}

func TestOutputToolDistinguishesRunningAndTerminalResult(t *testing.T) {
	controller := &fakeController{instance: model.Instance{AgentID: "agent-1", ParentRunID: "parent-1", SessionID: "session-1", TenantID: "tenant-1", Status: model.StatusRunning}}
	outputTool, _, err := New(controller)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "parent-1")
	ctx = contextutil.WithSessionID(ctx, "session-1")
	ctx = contextutil.WithTenantID(ctx, "tenant-1")
	raw, err := outputTool.Execute(ctx, `{"agent_id":"agent-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	var running output
	if err := json.Unmarshal([]byte(raw), &running); err != nil {
		t.Fatal(err)
	}
	if running.RetrievalStatus != "not_ready" || running.Result != nil {
		t.Fatalf("unexpected running output: %+v", running)
	}

	controller.instance.Status = model.StatusCompleted
	controller.instance.Result = &model.TaskResult{ResultID: "result-1", Status: model.ResultStatusCompleted, Summary: "safe summary"}
	raw, err = outputTool.Execute(ctx, `{"agent_id":"agent-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	var completed output
	if err := json.Unmarshal([]byte(raw), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.RetrievalStatus != "ready" || completed.Result == nil || completed.Result.Summary != "safe summary" || !completed.ResultDelivered {
		t.Fatalf("unexpected completed output: %+v", completed)
	}
}

func TestOutputToolSuppressesDuplicateTerminalResult(t *testing.T) {
	controller := &fakeController{instance: model.Instance{
		AgentID:     "agent-1",
		ParentRunID: "parent-1",
		SessionID:   "session-1",
		TenantID:    "tenant-1",
		Status:      model.StatusCompleted,
		Result:      &model.TaskResult{ResultID: "result-1", Status: model.ResultStatusCompleted, Summary: "safe summary"},
	}}
	outputTool, _, err := New(controller)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "parent-1")
	ctx = contextutil.WithSessionID(ctx, "session-1")
	ctx = contextutil.WithTenantID(ctx, "tenant-1")
	if _, err := outputTool.Execute(ctx, `{"agent_id":"agent-1"}`); err != nil {
		t.Fatal(err)
	}
	raw, err := outputTool.Execute(ctx, `{"agent_id":"agent-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	var duplicate output
	if err := json.Unmarshal([]byte(raw), &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.RetrievalStatus != "ready" || !duplicate.DuplicateResult || duplicate.Result != nil {
		t.Fatalf("duplicate result should not be returned again: %+v", duplicate)
	}
}

func TestOutputToolRejectsTerminalResultWithoutResultID(t *testing.T) {
	controller := &fakeController{instance: model.Instance{
		AgentID:     "agent-1",
		ParentRunID: "parent-1",
		SessionID:   "session-1",
		TenantID:    "tenant-1",
		Status:      model.StatusCompleted,
		Result:      &model.TaskResult{Status: model.ResultStatusCompleted, Summary: "safe summary"},
	}}
	outputTool, _, err := New(controller)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "parent-1")
	ctx = contextutil.WithSessionID(ctx, "session-1")
	ctx = contextutil.WithTenantID(ctx, "tenant-1")
	if _, err := outputTool.Execute(ctx, `{"agent_id":"agent-1"}`); err == nil {
		t.Fatal("expected missing result_id to be rejected")
	}
}

func TestOutputToolRejectsDifferentParentOwnership(t *testing.T) {
	controller := &fakeController{instance: model.Instance{AgentID: "agent-1", ParentRunID: "parent-1", SessionID: "session-1", TenantID: "tenant-1", Status: model.StatusRunning}}
	outputTool, _, err := New(controller)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "parent-2")
	ctx = contextutil.WithSessionID(ctx, "session-1")
	ctx = contextutil.WithTenantID(ctx, "tenant-1")
	if _, err := outputTool.Execute(ctx, `{"agent_id":"agent-1"}`); err == nil {
		t.Fatal("expected cross-parent access to be rejected")
	}
}

func TestOutputToolReturnsTimeoutWithoutStoppingChild(t *testing.T) {
	controller := &fakeController{instance: model.Instance{AgentID: "agent-1", ParentRunID: "parent-1", SessionID: "session-1", TenantID: "tenant-1", Status: model.StatusRunning}}
	outputTool, _, err := New(controller)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "parent-1")
	ctx = contextutil.WithSessionID(ctx, "session-1")
	ctx = contextutil.WithTenantID(ctx, "tenant-1")
	raw, err := outputTool.Execute(ctx, `{"agent_id":"agent-1","block":true,"timeout_seconds":1}`)
	if err != nil {
		t.Fatal(err)
	}
	var timedOut output
	if err := json.Unmarshal([]byte(raw), &timedOut); err != nil {
		t.Fatal(err)
	}
	if timedOut.RetrievalStatus != "timeout" || controller.instance.Status != model.StatusRunning {
		t.Fatalf("short wait changed child lifecycle: %+v", timedOut)
	}
}

func TestOutputToolRechecksOwnershipWhileBlocking(t *testing.T) {
	controller := &sequenceController{values: []model.Instance{
		{AgentID: "agent-1", ParentRunID: "parent-1", SessionID: "session-1", TenantID: "tenant-1", Status: model.StatusRunning},
		{AgentID: "agent-1", ParentRunID: "parent-2", SessionID: "session-1", TenantID: "tenant-1", Status: model.StatusCompleted, Result: &model.TaskResult{ResultID: "result-1"}},
	}}
	outputTool, _, err := New(controller)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "parent-1")
	ctx = contextutil.WithSessionID(ctx, "session-1")
	ctx = contextutil.WithTenantID(ctx, "tenant-1")
	if _, err := outputTool.Execute(ctx, `{"agent_id":"agent-1","block":true,"timeout_seconds":1}`); err == nil {
		t.Fatal("expected ownership drift during blocking wait to be rejected")
	}
}

func TestStopToolWritesCancellationIntentWhenStoreInjected(t *testing.T) {
	controller := &fakeController{instance: model.Instance{AgentID: "agent-1", ParentRunID: "parent-1", SessionID: "session-1", TenantID: "tenant-1", Status: model.StatusRunning}}
	cancels := &recordingCancellationStore{}
	_, stopTool, err := New(controller, WithCancellationStore(cancels))
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "parent-1")
	ctx = contextutil.WithSessionID(ctx, "session-1")
	ctx = contextutil.WithTenantID(ctx, "tenant-1")
	if _, err := stopTool.Execute(ctx, `{"agent_id":"agent-1"}`); err != nil {
		t.Fatal(err)
	}
	if cancels.agentID != "agent-1" || cancels.runID != "parent-1" {
		t.Fatalf("stop intent was not recorded: %+v", cancels)
	}
	if controller.stopped {
		t.Fatal("TaskStop should not bypass cancellation store")
	}
}

var _ contract.Controller = (*fakeController)(nil)
var _ contract.Controller = (*sequenceController)(nil)
var _ contract.CancellationStore = (*recordingCancellationStore)(nil)
