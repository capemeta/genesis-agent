package service

import (
	"context"
	"testing"
	"time"

	artifactmemory "genesis-agent/internal/capabilities/artifact/adapter/memory"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestCompletionEvaluatorRequiresExactPersistentFacts(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	store := artifactmemory.NewStore()
	evaluator, err := NewCompletionEvaluator(store, store, store, store, store)
	if err != nil {
		t.Fatal(err)
	}
	spec := artifactmodel.DeliverableSpec{ID: "deck", TenantID: "tenant", RunID: "run", Required: true, Role: artifactmodel.DeliverableRolePrimary, DesiredName: "deck.pptx", QAPolicy: "ppt-visual", DeliveryPolicy: "inbox", CreatedAt: now}
	if err := store.CreateDeliverable(ctx, spec); err != nil {
		t.Fatal(err)
	}
	assertIncomplete := func(stage string) {
		t.Helper()
		got, err := evaluator.EvaluateCompletion(ctx, "tenant", "run")
		if err != nil {
			t.Fatal(err)
		}
		if got.Complete {
			t.Fatalf("%s: expected incomplete", stage)
		}
	}
	assertIncomplete("spec only")
	selection := artifactmodel.DeliverableSelection{DeliverableID: "deck", ProducedResourceID: "produced-1", SelectedBy: "harness", CreatedAt: now}
	if err := store.CreateSelection(ctx, "tenant", "run", selection); err != nil {
		t.Fatal(err)
	}
	publication := artifactmodel.ArtifactPublicationRecord{ID: "publication-1", TenantID: "tenant", RunID: "run", ProducedResourceID: "produced-1", DeliverableID: "deck", DesiredName: "deck.pptx", ArtifactID: "artifact-1", SubjectVersion: "v1", SubjectSHA256: "sha", GateVersion: "gate-v1", IdempotencyKey: "pk", Status: artifactmodel.PublicationCommitted, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, _, err := store.CreatePublication(ctx, publication); err != nil {
		t.Fatal(err)
	}
	assertIncomplete("published only")
	delivery := artifactmodel.DeliveryRecord{ID: "delivery-1", TenantID: "tenant", RunID: "run", DeliverableID: "deck", PublicationID: "publication-1", ArtifactID: "artifact-1", Target: workmodel.ResourceRef{Authority: "product", Scheme: "inbox", ID: "target", Scope: workmodel.ResourceScope{TenantID: "tenant"}}, TargetKind: artifactmodel.DeliveryProductInbox, TargetName: "deck.pptx", ResultResource: workmodel.ResourceRef{Authority: "product", Scheme: "delivery", ID: "result", Scope: workmodel.ResourceScope{TenantID: "tenant"}}, IdempotencyKey: "dk", Status: artifactmodel.DeliverySucceeded, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, _, err := store.CreateDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	assertIncomplete("qa missing")
	wrong := artifactmodel.QAEvidenceRecord{ID: "qa-old", TenantID: "tenant", RunID: "run", DeliverableID: "deck", ProducedResourceID: "produced-1", PublicationID: "publication-1", SubjectVersion: "old", SubjectSHA256: "sha", PolicyID: "ppt-visual", Validator: "render", ValidatorVersion: "1", Status: artifactmodel.QAEvidencePassed, CreatedAt: now}
	if err := store.CreateQAEvidence(ctx, wrong); err != nil {
		t.Fatal(err)
	}
	assertIncomplete("stale qa")
	valid := wrong
	valid.ID = "qa-current"
	valid.SubjectVersion = "v1"
	if err := store.CreateQAEvidence(ctx, valid); err != nil {
		t.Fatal(err)
	}
	got, err := evaluator.EvaluateCompletion(ctx, "tenant", "run")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Complete {
		t.Fatalf("expected complete, got %+v", got)
	}
}

func TestCompletionEvaluatorIgnoresOptionalDeliverables(t *testing.T) {
	store := artifactmemory.NewStore()
	now := time.Now().UTC()
	if err := store.CreateDeliverable(context.Background(), artifactmodel.DeliverableSpec{ID: "thumb", TenantID: "tenant", RunID: "run", Required: false, Role: artifactmodel.DeliverableRoleSupporting, DeliveryPolicy: "none", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	evaluator, _ := NewCompletionEvaluator(store, store, store, store, store)
	got, err := evaluator.EvaluateCompletion(context.Background(), "tenant", "run")
	if err != nil || !got.Complete {
		t.Fatalf("optional deliverable must not block: got=%+v err=%v", got, err)
	}
}
