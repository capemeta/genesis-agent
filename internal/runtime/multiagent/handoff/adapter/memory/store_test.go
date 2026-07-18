package memory

import (
	"context"
	"testing"

	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/runtime/multiagent/handoff"
)

func TestStoreIsTenantScopedAndReturnsCopies(t *testing.T) {
	store := New()
	receipt := handoff.Receipt{
		ID: "receipt", TenantID: "tenant-a", IdempotencyKey: "key",
		Resources: []workmodel.ResourceRef{{ID: "resource"}},
		Artifacts: []artifactmodel.ArtifactRef{{ID: "artifact"}},
	}
	stored, created, err := store.PutIfAbsent(context.Background(), receipt)
	if err != nil || !created {
		t.Fatalf("first put failed: created=%v err=%v", created, err)
	}
	stored.Resources[0].ID = "mutated"
	stored.Artifacts[0].ID = "mutated"
	again, created, err := store.PutIfAbsent(context.Background(), receipt)
	if err != nil || created || again.Resources[0].ID != "resource" || again.Artifacts[0].ID != "artifact" {
		t.Fatalf("store leaked mutable slices: created=%v receipt=%+v err=%v", created, again, err)
	}

	other := receipt
	other.ID = "receipt-b"
	other.TenantID = "tenant-b"
	if _, created, err := store.PutIfAbsent(context.Background(), other); err != nil || !created {
		t.Fatalf("tenant isolation failed: created=%v err=%v", created, err)
	}

	collision := receipt
	collision.IdempotencyKey = "another-key"
	if _, _, err := store.PutIfAbsent(context.Background(), collision); err == nil {
		t.Fatal("same tenant receipt id collision must fail")
	}
}
