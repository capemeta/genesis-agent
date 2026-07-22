package subagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	skillmodel "genesis-agent/internal/capabilities/skill/model"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

func TestFileStorePersistsInstanceForAnotherStore(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	value := contract.StoredInstance{
		Instance: model.Instance{AgentID: "agent-test-1", ParentRunID: "parent", SessionID: "session", TenantID: "tenant", Status: model.StatusCompleted, Result: &model.TaskResult{Summary: "done"}},
		Request:  contract.SpawnRequest{SessionID: "session", TenantID: "tenant", ParentRunID: "parent", Prompt: "inspect", Agent: &domain.Agent{Name: "explore"}},
	}
	if err := store.Save(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	another, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := another.Get(context.Background(), "agent-test-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Instance.Result == nil || got.Instance.Result.Summary != "done" || got.Request.Agent.Name != "explore" {
		t.Fatalf("unexpected stored instance: %+v", got)
	}
}

func TestFileStoreInvocationClaimIsIdempotentAcrossStoreInstances(t *testing.T) {
	root := t.TempDir()
	firstStore, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	first := contract.StoredInstance{
		Instance: model.Instance{AgentID: "agent-first", Status: model.StatusRunning},
		Request:  contract.SpawnRequest{TenantID: "tenant", ParentRunID: "parent", InvocationBinding: skillmodel.InvocationBinding{ID: "binding-1"}},
	}
	stored, created, err := firstStore.SaveIfInvocationAbsent(context.Background(), first)
	if err != nil || !created || stored.Instance.AgentID != first.Instance.AgentID {
		t.Fatalf("first claim stored=%+v created=%v err=%v", stored, created, err)
	}
	first.Instance.Status = model.StatusCompleted
	if err := firstStore.Save(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	secondStore, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	replay := first
	replay.Instance.AgentID = "agent-second"
	stored, created, err = secondStore.SaveIfInvocationAbsent(context.Background(), replay)
	if err != nil || created || stored.Instance.AgentID != "agent-first" || stored.Instance.Status != model.StatusCompleted {
		t.Fatalf("replay stored=%+v created=%v err=%v", stored, created, err)
	}
}

func TestFileStoreRejectsUnsafeAgentID(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = store.Save(context.Background(), contract.StoredInstance{Instance: model.Instance{AgentID: `..\escape`}})
	if err == nil || !strings.Contains(err.Error(), "非法") {
		t.Fatalf("expected unsafe id rejection, got %v", err)
	}
	if _, err := store.Get(context.Background(), filepath.ToSlash("../escape")); err == nil {
		t.Fatal("expected unsafe get id rejection")
	}
}

func TestFileStoreRejectsMismatchedRecordAgentID(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), contract.StoredInstance{Instance: model.Instance{AgentID: "agent-good", Status: model.StatusCompleted}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".genesis", "runtime", "subagents", "agent-good.json")
	raw, err := json.Marshal(record{SchemaVersion: storeSchemaVersion, Stored: contract.StoredInstance{Instance: model.Instance{AgentID: "agent-other", Status: model.StatusCompleted}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), "agent-good"); err == nil {
		t.Fatal("expected mismatched record rejection")
	}
}

func TestFileStoreListsNewestFirst(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"agent-old", "agent-new"} {
		if err := store.Save(context.Background(), contract.StoredInstance{Instance: model.Instance{AgentID: id, Status: model.StatusCompleted}}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	items, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Stored.Instance.AgentID != "agent-new" || items[1].Stored.Instance.AgentID != "agent-old" {
		t.Fatalf("unexpected list order: %+v", items)
	}
}

func TestFileStoreCleanupKeepsRunningByDefault(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	values := []contract.StoredInstance{
		{Instance: model.Instance{AgentID: "agent-done", Status: model.StatusCompleted, CreatedAt: old}},
		{Instance: model.Instance{AgentID: "agent-running", Status: model.StatusRunning, CreatedAt: old}},
	}
	for _, value := range values {
		if err := store.Save(context.Background(), value); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, ".genesis", "runtime", "subagents", value.Instance.AgentID+".json")
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.Cleanup(context.Background(), CleanupOptions{OlderThan: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 || result.Errors != 0 {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}
	if _, err := store.Get(context.Background(), "agent-done"); err == nil {
		t.Fatal("expected completed record to be deleted")
	}
	if _, err := store.Get(context.Background(), "agent-running"); err != nil {
		t.Fatalf("running record should be kept: %v", err)
	}
}

func TestFileStoreResultDeliveryIsPersistent(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	key := contract.ResultDeliveryKey{
		TenantID:    "tenant",
		SessionID:   "session",
		ParentRunID: "parent",
		AgentID:     "agent-test",
		ResultID:    "result-test",
	}
	duplicate, err := store.MarkDelivered(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate {
		t.Fatal("first delivery should not be duplicate")
	}
	another, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err = another.MarkDelivered(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate {
		t.Fatal("second delivery should be duplicate across store instances")
	}
}
