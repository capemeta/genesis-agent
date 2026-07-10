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
