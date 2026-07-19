package permission

import (
	"context"
	"path/filepath"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
)

func TestFileProjectGrantStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grants.yaml")
	store, err := NewFileProjectGrantStore(path)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "docs")
	grants := []RuntimeGrant{{
		Action: approvalmodel.ActionFileRead,
		Scope:  approvalmodel.GrantScopeProject,
		Path:   root,
	}}
	if err := store.Save(context.Background(), grants); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded = %+v, want 1 grant", loaded)
	}
	if loaded[0].Action != approvalmodel.ActionFileRead || loaded[0].Scope != approvalmodel.GrantScopeProject {
		t.Fatalf("loaded grant = %+v", loaded[0])
	}
	if normalizeGrantPath(loaded[0].Path) != normalizeGrantPath(root) {
		t.Fatalf("path = %q, want %q", loaded[0].Path, root)
	}
}

func TestFileProjectGrantStoreMissingFile(t *testing.T) {
	store, err := NewFileProjectGrantStore(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded = %+v, want empty", loaded)
	}
}
