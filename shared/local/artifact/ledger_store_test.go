package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestLedgerStorePersistsAllControlRecordsAcrossRestart(t *testing.T) {
	root := t.TempDir()
	store := mustLedger(t, root)
	now := time.Now().UTC()
	spec := artifactmodel.DeliverableSpec{ID: "deck", TenantID: "tenant", RunID: "run", Required: true, Role: artifactmodel.DeliverableRolePrimary, DeliveryPolicy: "download", CreatedAt: now}
	if err := store.CreateDeliverable(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	selection := artifactmodel.DeliverableSelection{DeliverableID: "deck", ProducedResourceID: "produced", SelectedBy: "harness", CreatedAt: now}
	if err := store.CreateSelection(context.Background(), "tenant", "run", selection); err != nil {
		t.Fatal(err)
	}
	reservation := artifactmodel.OutputReservation{ID: "reservation", TenantID: "tenant", RunID: "run", BindingID: "binding", DeliverableID: "deck", AttemptID: "attempt", LogicalTarget: workmodel.WorkspacePath("output/deck.pptx"), CreatedAt: now}
	if err := store.CreateReservation(context.Background(), reservation); err != nil {
		t.Fatal(err)
	}
	evidence := artifactmodel.QAEvidenceRecord{ID: "qa", TenantID: "tenant", RunID: "run", DeliverableID: "deck", ProducedResourceID: "produced", SubjectVersion: "v1", SubjectSHA256: "sha", PolicyID: "policy", Validator: "validator", ValidatorVersion: "v1", Status: artifactmodel.QAEvidencePassed, CreatedAt: now}
	if err := store.CreateQAEvidence(context.Background(), evidence); err != nil {
		t.Fatal(err)
	}
	publication := ledgerPublication(now)
	if _, created, err := store.CreatePublication(context.Background(), publication); err != nil || !created {
		t.Fatalf("create publication: created=%v err=%v", created, err)
	}
	delivery := ledgerDelivery(now)
	if _, created, err := store.CreateDelivery(context.Background(), delivery); err != nil || !created {
		t.Fatalf("create delivery: created=%v err=%v", created, err)
	}

	restarted := mustLedger(t, root)
	if values, err := restarted.ListDeliverables(context.Background(), "tenant", "run"); err != nil || len(values) != 1 {
		t.Fatalf("deliverables=%v err=%v", values, err)
	}
	if got, err := restarted.GetSelection(context.Background(), "tenant", "run", "deck"); err != nil || got.ProducedResourceID != "produced" {
		t.Fatalf("selection=%+v err=%v", got, err)
	}
	if values, err := restarted.ListReservations(context.Background(), "tenant", "run", ""); err != nil || len(values) != 1 {
		t.Fatalf("reservations=%v err=%v", values, err)
	}
	if values, err := restarted.ListQAEvidence(context.Background(), "tenant", "run", ""); err != nil || len(values) != 1 {
		t.Fatalf("evidence=%v err=%v", values, err)
	}
	if _, err := restarted.GetPublicationByIdempotencyKey(context.Background(), "tenant", "publication-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.GetDeliveryByIdempotencyKey(context.Background(), "tenant", "delivery-key"); err != nil {
		t.Fatal(err)
	}
	if values, err := restarted.ListPublications(context.Background(), "other", "run", ""); err != nil || len(values) != 0 {
		t.Fatalf("cross-tenant publications=%v err=%v", values, err)
	}
}

func TestLedgerStoreIdempotencyCASAndStatusTransitionsSurviveRestart(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	store := mustLedger(t, root)
	publication := ledgerPublication(now)
	if _, _, err := store.CreatePublication(context.Background(), publication); err != nil {
		t.Fatal(err)
	}
	next := publication
	next.Status = artifactmodel.PublicationStaging
	next.UpdatedAt = now.Add(time.Second)
	updated, err := store.UpdatePublication(context.Background(), 1, next)
	if err != nil || updated.Revision != 2 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	restarted := mustLedger(t, root)
	if replay, created, err := restarted.CreatePublication(context.Background(), publication); err != nil || created || replay.Revision != 2 {
		t.Fatalf("replay=%+v created=%v err=%v", replay, created, err)
	}
	if _, err := restarted.UpdatePublication(context.Background(), 1, next); !errors.Is(err, artifactcontract.ErrRevisionConflict) {
		t.Fatalf("expected CAS conflict, got %v", err)
	}
	conflict := publication
	conflict.DesiredName = "other.pptx"
	if _, _, err := restarted.CreatePublication(context.Background(), conflict); !errors.Is(err, artifactcontract.ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
	back := updated
	back.Status = artifactmodel.PublicationPending
	back.UpdatedAt = now.Add(2 * time.Second)
	if _, err := restarted.UpdatePublication(context.Background(), 2, back); !errors.Is(err, artifactcontract.ErrRevisionConflict) {
		t.Fatalf("expected transition conflict, got %v", err)
	}
}

func TestLedgerStoreCoordinatesConcurrentInstances(t *testing.T) {
	root := t.TempDir()
	first := mustLedger(t, root)
	second := mustLedger(t, root)
	now := time.Now().UTC()
	record := ledgerPublication(now)
	var created, failed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := first
			if i%2 == 1 {
				store = second
			}
			_, fresh, err := store.CreatePublication(context.Background(), record)
			if err != nil {
				failed.Add(1)
			} else if fresh {
				created.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if created.Load() != 1 || failed.Load() != 0 {
		t.Fatalf("created=%d failed=%d", created.Load(), failed.Load())
	}
}

func TestLedgerStoreFailsClosedForCorruptOrInconsistentFile(t *testing.T) {
	for name, content := range map[string]string{
		"invalid-json":   `{`,
		"unknown-schema": `{"schema_version":99}`,
		"invalid-record": `{"schema_version":1,"deliverables":[{"id":"broken"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "artifacts", "specs")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "ledger.json"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewLedgerStore(root); err == nil {
				t.Fatal("expected corrupt ledger error")
			}
		})
	}
}

func mustLedger(t *testing.T, root string) *LedgerStore {
	t.Helper()
	store, err := NewLedgerStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func ledgerPublication(now time.Time) artifactmodel.ArtifactPublicationRecord {
	return artifactmodel.ArtifactPublicationRecord{ID: "publication", TenantID: "tenant", RunID: "run", ProducedResourceID: "produced", DeliverableID: "deck", DesiredName: "deck.pptx", GateVersion: "gate-v1", IdempotencyKey: "publication-key", Status: artifactmodel.PublicationPending, Revision: 1, CreatedAt: now, UpdatedAt: now}
}
func ledgerDelivery(now time.Time) artifactmodel.DeliveryRecord {
	return artifactmodel.DeliveryRecord{ID: "delivery", TenantID: "tenant", RunID: "run", DeliverableID: "deck", PublicationID: "publication", ArtifactID: "artifact", Target: workmodel.ResourceRef{Authority: "host", Scheme: "directory", ID: "inbox"}, TargetKind: artifactmodel.DeliveryProductInbox, TargetName: "deck.pptx", IdempotencyKey: "delivery-key", Status: artifactmodel.DeliveryPending, Revision: 1, CreatedAt: now, UpdatedAt: now}
}
