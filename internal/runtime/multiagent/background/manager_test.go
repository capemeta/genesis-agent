package background

import (
	"context"
	"sync"
	"testing"
	"time"

	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

type fakeBackgroundController struct {
	mu       sync.Mutex
	instance model.Instance
	stops    int
}

func (f *fakeBackgroundController) Spawn(context.Context, contract.SpawnRequest) (model.Instance, error) {
	return f.instance, nil
}
func (f *fakeBackgroundController) Wait(context.Context, string) (model.Instance, error) {
	return f.instance, nil
}
func (f *fakeBackgroundController) Resume(context.Context, string, string) (model.Instance, error) {
	return f.instance, nil
}
func (f *fakeBackgroundController) Stop(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.instance.Status = model.StatusCancelled
	return nil
}
func (f *fakeBackgroundController) Get(context.Context, string) (model.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.instance, nil
}

func TestManagerConsumesStopIntentAndReleasesLease(t *testing.T) {
	store := NewMemoryControlStore()
	controller := &fakeBackgroundController{instance: model.Instance{AgentID: "agent-1", Status: model.StatusRunning}}
	manager, err := New(Deps{
		Controller: controller,
		Leases:     store,
		Heartbeats: store,
		Cancels:    store,
		OwnerID:    "worker-1",
		Interval:   10 * time.Millisecond,
		LeaseTTL:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- manager.Run(ctx, "agent-1") }()

	for {
		if hb, _ := store.LastHeartbeat(context.Background(), "agent-1"); !hb.IsZero() {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("manager stopped before heartbeat: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if err := store.RequestStop(context.Background(), "agent-1", "parent-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if controller.stops != 1 {
		t.Fatalf("expected one stop, got %d", controller.stops)
	}
	if stop, err := store.PollStop(context.Background(), "agent-1", "worker-1"); err != nil || stop {
		t.Fatalf("stop intent should be cleared, stop=%v err=%v", stop, err)
	}
	if ok, err := store.Acquire(context.Background(), contract.Lease{AgentID: "agent-1", OwnerID: "worker-2", ExpiresAt: time.Now().Add(time.Second)}); err != nil || !ok {
		t.Fatalf("lease should be released, ok=%v err=%v", ok, err)
	}
}

var _ contract.Controller = (*fakeBackgroundController)(nil)
