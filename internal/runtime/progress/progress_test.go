package progress

import (
	"context"
	"testing"
)

func TestEmitRecoversSinkPanic(t *testing.T) {
	ctx := WithSink(context.Background(), func(Event) {
		panic("boom")
	})
	Emit(ctx, Event{Kind: KindRun, Phase: PhaseStart})
}

func TestEmitFillsDefaults(t *testing.T) {
	var got Event
	ctx := WithSink(context.Background(), func(event Event) {
		got = event
	})
	Emit(ctx, Event{Kind: KindTool, Phase: PhaseStart})
	if got.Level != LevelInfo {
		t.Fatalf("Level=%s", got.Level)
	}
	if got.Time.IsZero() {
		t.Fatal("Time was not set")
	}
}

func TestChildBridgeRoundTrip(t *testing.T) {
	var called bool
	bridge := func(Event) { called = true }
	ctx := WithChildBridge(context.Background(), bridge)
	got := ChildBridgeFromContext(ctx)
	if got == nil {
		t.Fatal("expected child bridge")
	}
	got(Event{})
	if !called {
		t.Fatal("bridge was not invoked")
	}
	if ChildBridgeFromContext(context.Background()) != nil {
		t.Fatal("empty context should have no bridge")
	}
}
