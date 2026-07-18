package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestSelectionAndReservationAreExclusive(t *testing.T) {
	store := NewStore()
	now := time.Now().UTC()
	spec := artifactmodel.DeliverableSpec{ID: "d1", TenantID: "t", RunID: "r", Required: true, Role: artifactmodel.DeliverableRolePrimary, DeliveryPolicy: "download", CreatedAt: now}
	if err := store.CreateDeliverable(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	selection := artifactmodel.DeliverableSelection{DeliverableID: "d1", ProducedResourceID: "p1", SelectedBy: "harness", CreatedAt: now}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if store.CreateSelection(context.Background(), "t", "r", selection) == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("selection wins=%d", wins.Load())
	}
	reservation := artifactmodel.OutputReservation{ID: "o1", TenantID: "t", RunID: "r", BindingID: "b", DeliverableID: "d1", AttemptID: "a1", LogicalTarget: workmodel.WorkspacePath("output/deck.pptx"), CreatedAt: now}
	if err := store.CreateReservation(context.Background(), reservation); err != nil {
		t.Fatal(err)
	}
	reservation.ID = "o2"
	if err := store.CreateReservation(context.Background(), reservation); !errors.Is(err, artifactcontract.ErrAlreadyExists) {
		t.Fatalf("expected slot conflict: %v", err)
	}
}

func TestPublicationIdempotencyAndCAS(t *testing.T) {
	store := NewStore()
	now := time.Now().UTC()
	record := publication(now)
	var created atomic.Int32
	var failed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, fresh, err := store.CreatePublication(context.Background(), record)
			if err != nil {
				failed.Add(1)
			} else if fresh {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 || failed.Load() != 0 {
		t.Fatalf("created=%d failed=%d", created.Load(), failed.Load())
	}
	conflict := record
	conflict.DesiredName = "other.pptx"
	if _, _, err := store.CreatePublication(context.Background(), conflict); !errors.Is(err, artifactcontract.ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict: %v", err)
	}
	next := record
	next.Status = artifactmodel.PublicationStaging
	next.UpdatedAt = now.Add(time.Second)
	updated, err := store.UpdatePublication(context.Background(), 1, next)
	if err != nil || updated.Revision != 2 {
		t.Fatalf("update=%+v err=%v", updated, err)
	}
	if _, err := store.UpdatePublication(context.Background(), 1, next); !errors.Is(err, artifactcontract.ErrRevisionConflict) {
		t.Fatalf("expected CAS conflict: %v", err)
	}
	if replay, fresh, err := store.CreatePublication(context.Background(), record); err != nil || fresh || replay.Revision != 2 {
		t.Fatalf("progressed idempotent replay=%+v fresh=%v err=%v", replay, fresh, err)
	}
	gated := updated
	gated.Status = artifactmodel.PublicationGated
	gated.SubjectVersion = "sha256:source"
	gated.SubjectSHA256 = "content"
	gated.UpdatedAt = now.Add(2 * time.Second)
	gated, err = store.UpdatePublication(context.Background(), 2, gated)
	if err != nil {
		t.Fatal(err)
	}
	committed := gated
	committed.Status = artifactmodel.PublicationCommitted
	committed.ArtifactID = "artifact-1"
	committed.UpdatedAt = now.Add(3 * time.Second)
	committed, err = store.UpdatePublication(context.Background(), 3, committed)
	if err != nil {
		t.Fatal(err)
	}
	if replay, fresh, err := store.CreatePublication(context.Background(), record); err != nil || fresh || replay.Revision != 4 || replay.SubjectVersion != "sha256:source" {
		t.Fatalf("committed idempotent replay=%+v fresh=%v err=%v", replay, fresh, err)
	}
	back := committed
	back.Status = artifactmodel.PublicationPending
	back.UpdatedAt = now.Add(4 * time.Second)
	if _, err := store.UpdatePublication(context.Background(), 4, back); !errors.Is(err, artifactcontract.ErrRevisionConflict) {
		t.Fatalf("expected transition conflict: %v", err)
	}
}

func TestDeliveryCanRetryWithoutChangingPublication(t *testing.T) {
	store := NewStore()
	now := time.Now().UTC()
	record := delivery(now)
	if _, created, err := store.CreateDelivery(context.Background(), record); err != nil || !created {
		t.Fatal(err)
	}
	delivering := record
	delivering.Status = artifactmodel.DeliveryDelivering
	delivering.UpdatedAt = now.Add(time.Second)
	delivering, err := store.UpdateDelivery(context.Background(), 1, delivering)
	if err != nil {
		t.Fatal(err)
	}
	failed := delivering
	failed.Status = artifactmodel.DeliveryFailed
	failed.FailureCode = "TARGET_BUSY"
	failed.UpdatedAt = now.Add(2 * time.Second)
	failed, err = store.UpdateDelivery(context.Background(), 2, failed)
	if err != nil {
		t.Fatal(err)
	}
	retry := failed
	retry.Status = artifactmodel.DeliveryDelivering
	retry.FailureCode = ""
	retry.UpdatedAt = now.Add(3 * time.Second)
	retry, err = store.UpdateDelivery(context.Background(), 3, retry)
	if err != nil || retry.Revision != 4 {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
}

func publication(now time.Time) artifactmodel.ArtifactPublicationRecord {
	return artifactmodel.ArtifactPublicationRecord{ID: "pub1", TenantID: "t", RunID: "r", ProducedResourceID: "p", DeliverableID: "d", DesiredName: "deck.pptx", GateVersion: "g1", IdempotencyKey: "key", Status: artifactmodel.PublicationPending, Revision: 1, CreatedAt: now, UpdatedAt: now}
}
func delivery(now time.Time) artifactmodel.DeliveryRecord {
	return artifactmodel.DeliveryRecord{ID: "del1", TenantID: "t", RunID: "r", DeliverableID: "d", PublicationID: "pub1", ArtifactID: "art1", Target: workmodel.ResourceRef{Authority: "host", Scheme: "directory", ID: "inbox"}, TargetKind: artifactmodel.DeliveryProductInbox, TargetName: "artifact.bin", IdempotencyKey: "delivery-key", Status: artifactmodel.DeliveryPending, Revision: 1, CreatedAt: now, UpdatedAt: now}
}
