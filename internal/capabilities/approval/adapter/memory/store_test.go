package memory

import (
	"context"
	"testing"

	"genesis-agent/internal/capabilities/approval/model"
)

func TestStoreSessionDecision(t *testing.T) {
	store := NewStore()
	key := model.GrantKey{Action: model.ActionFileWrite, ResourceURI: "workspace://a.txt", Scope: model.GrantScopeSession}
	decision := model.Decision{Type: model.DecisionApprovedForScope, Scope: model.GrantScopeSession}
	if err := store.Put(context.Background(), key, decision); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Type != decision.Type {
		t.Fatalf("Get = %#v, %v, want stored decision", got, ok)
	}
}

func TestStoreIgnoresOnceScope(t *testing.T) {
	store := NewStore()
	key := model.GrantKey{Action: model.ActionFileWrite, ResourceURI: "workspace://a.txt", Scope: model.GrantScopeOnce}
	if err := store.Put(context.Background(), key, model.Decision{Type: model.DecisionApproved, Scope: model.GrantScopeOnce}); err != nil {
		t.Fatal(err)
	}
	_, ok, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("once scope should not be cached")
	}
}
