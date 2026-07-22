package subagent

import (
	"context"
	"sync"
	"testing"

	skillmodel "genesis-agent/internal/capabilities/skill/model"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

func TestStorePersistsAndClaimsInvocationExactlyOnce(t *testing.T) {
	root := t.TempDir()
	first, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	value := stored("agent-1")
	got, created, err := first.SaveIfInvocationAbsent(context.Background(), value)
	if err != nil || !created || got.Instance.AgentID != "agent-1" {
		t.Fatalf("got=%+v created=%v err=%v", got, created, err)
	}
	value.Instance.Status = model.StatusCompleted
	if err := first.Save(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	replay := stored("agent-2")
	got, created, err = second.SaveIfInvocationAbsent(context.Background(), replay)
	if err != nil || created || got.Instance.AgentID != "agent-1" || got.Instance.Status != model.StatusCompleted {
		t.Fatalf("replay=%+v created=%v err=%v", got, created, err)
	}
}

func TestStoreConcurrentInvocationClaimHasSingleWinner(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	created := make(chan bool, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, won, err := store.SaveIfInvocationAbsent(context.Background(), stored("agent-"+string(rune('a'+i))))
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			created <- won
		}(i)
	}
	wg.Wait()
	close(created)
	winners := 0
	for won := range created {
		if won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("winners=%d", winners)
	}
}

func stored(agentID string) contract.StoredInstance {
	return contract.StoredInstance{
		Instance: model.Instance{AgentID: agentID, TenantID: "tenant", ParentRunID: "parent", Status: model.StatusRunning},
		Request:  contract.SpawnRequest{TenantID: "tenant", ParentRunID: "parent", InvocationBinding: skillmodel.InvocationBinding{ID: "binding"}},
	}
}
