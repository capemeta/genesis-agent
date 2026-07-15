package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"genesis-agent/internal/capabilities/mcp/adapter/store"
	"genesis-agent/internal/capabilities/mcp/contract"
)

func TestFileApprovalStoreRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "mcp-approvals.json")
	s, err := store.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, ok, err := s.Get(ctx, "demo"); err != nil || ok {
		t.Fatalf("expected empty, ok=%v err=%v", ok, err)
	}
	if err := s.Put(ctx, "demo", contract.ApprovalApproved); err != nil {
		t.Fatal(err)
	}
	s2, err := store.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, ok, err := s2.Get(ctx, "demo")
	if err != nil || !ok || d != contract.ApprovalApproved {
		t.Fatalf("got %q ok=%v err=%v", d, ok, err)
	}
}
