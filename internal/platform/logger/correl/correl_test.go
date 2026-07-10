package correl_test

import (
	"context"
	"testing"

	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger/correl"
)

func TestEnrich_Priority(t *testing.T) {
	ctx := contextutil.WithRunID(context.Background(), "from-ctx")
	ctx = contextutil.WithSessionID(ctx, "sess-ctx")

	runID, sessionID, meta := correl.Enrich(ctx, "explicit", "", map[string]string{"run_id": "from-meta", "session_id": "sess-meta"})
	if runID != "explicit" {
		t.Fatalf("runID = %q, want explicit", runID)
	}
	if sessionID != "sess-ctx" {
		t.Fatalf("sessionID = %q, want sess-ctx", sessionID)
	}
	if meta["run_id"] != "explicit" || meta["session_id"] != "sess-ctx" {
		t.Fatalf("meta = %#v", meta)
	}
}
