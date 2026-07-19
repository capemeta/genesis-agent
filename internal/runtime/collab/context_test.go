package collab

import (
	"context"
	"errors"
	"testing"
)

func TestSessionModePrefersStore(t *testing.T) {
	store := NewMemoryStore()
	ctx := WithStore(context.Background(), store)
	if err := store.Set(ctx, "s1", SessionState{Mode: ModePlan}); err != nil {
		t.Fatal(err)
	}
	// context 仍为 default，但 Store 已是 plan
	ctx = WithMode(ctx, ModeDefault)
	mode, err := SessionMode(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if mode != ModePlan {
		t.Fatalf("got %s want plan_mode", mode)
	}
}

type failStore struct{}

func (failStore) Get(context.Context, string) (SessionState, error) {
	return SessionState{}, errors.New("disk failure")
}
func (failStore) Set(context.Context, string, SessionState) error { return nil }

func TestSessionModeFailClosed(t *testing.T) {
	ctx := WithStore(context.Background(), failStore{})
	_, err := SessionMode(ctx, "s1")
	if err == nil {
		t.Fatal("expected error")
	}
}
