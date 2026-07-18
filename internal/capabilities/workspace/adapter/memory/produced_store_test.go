package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestProducedResourceStoreExclusiveCreateAndTenantIsolation(t *testing.T) {
	store := NewProducedResourceStore()
	d := durableDescriptor("produced-1", "tenant-a", "run-1", "run:/work/binding/out.txt")
	if err := store.Create(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	conflict := durableDescriptor("produced-2", "tenant-a", "run-1", d.LogicalRef)
	if err := store.Create(context.Background(), conflict); !hasWorkspaceCode(err, workcontract.ErrCodeProducedResourceConflict) {
		t.Fatalf("logical conflict error = %v", err)
	}
	if _, err := store.Get(context.Background(), "tenant-b", "run-1", d.ID); !hasWorkspaceCode(err, workcontract.ErrCodeProducedResourceNotFound) {
		t.Fatalf("cross-tenant get error = %v", err)
	}
	got, err := store.GetByLogicalRef(context.Background(), "tenant-a", "run-1", d.LogicalRef)
	if err != nil || got.ID != d.ID {
		t.Fatalf("get by logical ref = %+v, %v", got, err)
	}
	next := durableDescriptor("produced-3", "tenant-a", "run-1", d.LogicalRef)
	next.Source.Version = "sha256:def"
	if err := store.UpsertCurrent(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListByRun(context.Background(), "tenant-a", "run-1")
	if err != nil || len(listed) != 1 || listed[0].ID != "produced-3" {
		t.Fatalf("list current heads = %+v, %v", listed, err)
	}
}

func durableDescriptor(id, tenant, runID, logical string) workmodel.ProducedResourceDescriptor {
	return workmodel.ProducedResourceDescriptor{ID: id, TenantID: tenant, RunID: runID, BindingID: "binding", LogicalRef: logical, Source: workmodel.ResourceRef{Authority: "host", Scheme: "run-file", ID: "locator-" + id, Version: "sha256:abc", Scope: workmodel.ResourceScope{TenantID: tenant}}, ObservedName: "out.txt", Size: 1, Availability: workmodel.ResourceAvailabilityDurable, CreatedAt: time.Now().UTC()}
}

func hasWorkspaceCode(err error, code workcontract.ErrorCode) bool {
	var classified *workcontract.Error
	return errors.As(err, &classified) && classified.Code == code
}
