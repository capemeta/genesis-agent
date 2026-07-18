package workspace

import (
	"context"
	"errors"
	"testing"
	"time"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestProducedResourceStoreSurvivesRestartAndRejectsLogicalConflict(t *testing.T) {
	root := t.TempDir()
	store, err := NewProducedResourceStore(root)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := localProducedDescriptor("produced-1", "run:/work/binding/out.txt")
	if err := store.Create(context.Background(), descriptor); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewProducedResourceStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Get(context.Background(), "tenant", "run", descriptor.ID)
	if err != nil || got.Source.ID != descriptor.Source.ID {
		t.Fatalf("restarted get = %+v, %v", got, err)
	}
	conflict := localProducedDescriptor("produced-2", descriptor.LogicalRef)
	if err := restarted.Create(context.Background(), conflict); !localWorkspaceErrorIs(err, workcontract.ErrCodeProducedResourceConflict) {
		t.Fatalf("logical conflict error = %v", err)
	}
	replaced := localProducedDescriptor("produced-3", descriptor.LogicalRef)
	replaced.Source.Version = "sha256:def"
	if err := restarted.UpsertCurrent(context.Background(), replaced); err != nil {
		t.Fatalf("upsert current error = %v", err)
	}
	head, err := restarted.GetByLogicalRef(context.Background(), "tenant", "run", descriptor.LogicalRef)
	if err != nil || head.ID != "produced-3" {
		t.Fatalf("upsert head = %+v, %v", head, err)
	}
	listed, err := restarted.ListByRun(context.Background(), "tenant", "run")
	if err != nil || len(listed) != 1 || listed[0].ID != "produced-3" {
		t.Fatalf("list current = %+v, %v", listed, err)
	}
	if _, err := restarted.Get(context.Background(), "tenant", "run", descriptor.ID); err != nil {
		t.Fatalf("old descriptor should remain: %v", err)
	}
	if _, err := restarted.Get(context.Background(), "other", "run", descriptor.ID); !localWorkspaceErrorIs(err, workcontract.ErrCodeProducedResourceNotFound) {
		t.Fatalf("cross tenant error = %v", err)
	}
	leaking := localProducedDescriptor("produced-3", "run:/work/binding/leak.txt")
	leaking.Source.Path = `C:\secret\leak.txt`
	if err := restarted.Create(context.Background(), leaking); !localWorkspaceErrorIs(err, workcontract.ErrCodeProducedResourceInvalid) {
		t.Fatalf("physical path leak error = %v", err)
	}
}

func localProducedDescriptor(id, logical string) workmodel.ProducedResourceDescriptor {
	return workmodel.ProducedResourceDescriptor{ID: id, TenantID: "tenant", RunID: "run", BindingID: "binding", LogicalRef: logical, Source: workmodel.ResourceRef{Authority: "host", Scheme: hostRunFileScheme, ID: "opaque", Version: "sha256:abc", Scope: workmodel.ResourceScope{TenantID: "tenant"}}, ObservedName: "out.txt", Size: 3, Availability: workmodel.ResourceAvailabilityDurable, CreatedAt: time.Now().UTC()}
}

func localWorkspaceErrorIs(err error, code workcontract.ErrorCode) bool {
	var classified *workcontract.Error
	return errors.As(err, &classified) && classified.Code == code
}
