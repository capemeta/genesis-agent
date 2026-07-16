package contextsnapshot

import (
	"context"
	"testing"

	memoryinmemory "genesis-agent/internal/capabilities/memory/adapter/inmemory"
	memory "genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
)

func TestContextSourceReturnsImmutableCopy(t *testing.T) {
	messages := []*domain.Message{domain.NewUserMessage("original")}
	ctx := WithParentSnapshot(context.Background(), messages, "call-1")
	messages[0].Content = "mutated after attach"

	snapshot, err := (ContextSource{}).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ToolCallID != "call-1" || len(snapshot.Messages) != 1 || snapshot.Messages[0].Content != "original" {
		t.Fatalf("unexpected immutable snapshot: %+v", snapshot)
	}
	snapshot.Messages[0].Content = "mutated by consumer"
	again, err := (ContextSource{}).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.Messages[0].Content != "original" {
		t.Fatalf("snapshot was modified by consumer: %+v", again)
	}
}

func TestContextSourceRejectsMissingSnapshot(t *testing.T) {
	if _, err := (ContextSource{}).Snapshot(context.Background()); err == nil {
		t.Fatal("expected missing snapshot error")
	}
}

func TestWithoutParentSnapshotHidesSourceValue(t *testing.T) {
	ctx := WithParentSnapshot(context.Background(), []*domain.Message{domain.NewUserMessage("private")}, "call-1")
	if _, err := (ContextSource{}).Snapshot(WithoutParentSnapshot(ctx)); err == nil {
		t.Fatal("expected cleared snapshot to be unavailable")
	}
}

func TestPersistentSourceMergesHistoryWithActiveRunSnapshot(t *testing.T) {
	store := memoryinmemory.NewInMemoryStore()
	ref := memory.SessionRef{TenantID: "tenant", UserID: "user", SessionID: "session"}
	oldUser := domain.NewUserMessage("old user")
	oldUser.UUID = "old-user"
	oldAnswer := domain.NewAssistantMessage("old answer")
	oldAnswer.UUID = "old-answer"
	if err := store.Append(context.Background(), ref, []*domain.Message{oldUser, oldAnswer}); err != nil {
		t.Fatal(err)
	}
	currentUser := domain.NewUserMessage("current user")
	currentUser.UUID = "current-user"
	taskCall := &domain.Message{UUID: "task-call", Role: domain.RoleAssistant, Kind: domain.MessageKindAssistant, ToolCalls: []domain.ToolCall{{ID: "call-1"}}}
	ctx := memory.ContextWithSessionRef(context.Background(), ref)
	ctx = WithParentSnapshot(ctx, []*domain.Message{domain.NewSystemMessage("runtime system"), oldUser, oldAnswer, currentUser, taskCall}, "call-1")

	snapshot, err := NewPersistentSource(store).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ToolCallID != "call-1" || len(snapshot.Messages) != 4 {
		t.Fatalf("unexpected merged snapshot: %+v", snapshot)
	}
	for i, want := range []string{"old user", "old answer", "current user", ""} {
		if snapshot.Messages[i].Content != want {
			t.Fatalf("message %d=%q want %q", i, snapshot.Messages[i].Content, want)
		}
	}
}

func TestPersistentSourceRejectsHistoryReadFailure(t *testing.T) {
	ctx := memory.ContextWithSessionRef(context.Background(), memory.SessionRef{SessionID: "session"})
	ctx = WithParentSnapshot(ctx, []*domain.Message{domain.NewUserMessage("current")}, "call-1")
	if _, err := NewPersistentSource(failingStore{}).Snapshot(ctx); err == nil {
		t.Fatal("expected persistent history error")
	}
}

func TestSessionRefCompletesPartialMemoryReference(t *testing.T) {
	ctx := contextutil.WithTenantID(context.Background(), "tenant-from-context")
	ctx = contextutil.WithUserID(ctx, "user-from-context")
	ctx = contextutil.WithSessionID(ctx, "session-from-context")
	ctx = memory.ContextWithSessionRef(ctx, memory.SessionRef{TenantID: "tenant-from-memory"})

	got := sessionRef(ctx)
	want := memory.SessionRef{
		TenantID:  "tenant-from-memory",
		UserID:    "user-from-context",
		SessionID: "session-from-context",
	}
	if got != want {
		t.Fatalf("sessionRef() = %#v, want %#v", got, want)
	}
}

func TestMergeMessagesDoesNotTreatDifferentToolCallsAsOverlap(t *testing.T) {
	persisted := []*domain.Message{{Role: domain.RoleAssistant, Kind: domain.MessageKindAssistant, ToolCalls: []domain.ToolCall{{ID: "old-call", Type: "function", Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"old"}`}}}}}
	active := []*domain.Message{{Role: domain.RoleAssistant, Kind: domain.MessageKindAssistant, ToolCalls: []domain.ToolCall{{ID: "new-call", Type: "function", Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"new"}`}}}}}
	merged := mergeMessages(persisted, active)
	if len(merged) != 2 || merged[1].ToolCalls[0].ID != "new-call" {
		t.Fatalf("different tool calls were incorrectly deduplicated: %+v", merged)
	}
}

type failingStore struct{}

func (failingStore) Append(context.Context, memory.SessionRef, []*domain.Message) error { return nil }
func (failingStore) GetRecent(context.Context, memory.SessionRef, memory.RecentOptions) (memory.RecentResult, error) {
	return memory.RecentResult{}, context.DeadlineExceeded
}
func (failingStore) Summarize(context.Context, memory.SessionRef, memory.SummarizeOptions) (memory.SummaryResult, error) {
	return memory.SummaryResult{}, nil
}
func (failingStore) GetSummary(context.Context, memory.SessionRef) (*domain.SessionSummary, error) {
	return nil, nil
}
func (failingStore) Clear(context.Context, memory.SessionRef) error { return nil }
