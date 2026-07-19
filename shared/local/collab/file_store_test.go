package collab

import (
	"context"
	"path/filepath"
	"testing"

	runtimecollab "genesis-agent/internal/runtime/collab"
)

func TestFileStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Set(ctx, "sess-1", runtimecollab.SessionState{Mode: runtimecollab.ModePlan, HandoffPending: true}); err != nil {
		t.Fatal(err)
	}
	st, err := store.Get(ctx, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != runtimecollab.ModePlan || !st.HandoffPending {
		t.Fatalf("got %+v", st)
	}
}

func TestWriteReadPlanDocument(t *testing.T) {
	root := t.TempDir()
	rel, err := WritePlanDocument(root, "abc", "# plan\n")
	if err != nil {
		t.Fatal(err)
	}
	if rel != ".genesis/plans/abc.md" {
		t.Fatalf("rel=%q", rel)
	}
	_, body, err := ReadPlanDocument(root, "abc")
	if err != nil || body != "# plan\n" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if _, err := filepath.Rel(root, filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}
