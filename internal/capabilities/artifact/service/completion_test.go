package service

import (
	"context"
	"testing"
	"time"

	artifactmemory "genesis-agent/internal/capabilities/artifact/adapter/memory"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmemory "genesis-agent/internal/capabilities/workspace/adapter/memory"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestCompletionEvaluatorRequiresExactPersistentFacts(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	store := artifactmemory.NewStore()
	evaluator, err := NewCompletionEvaluator(store, store, store, store, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := artifactmodel.DeliverableSpec{ID: "deck", TenantID: "tenant", RunID: "run", Required: true, Role: artifactmodel.DeliverableRolePrimary, DesiredName: "deck.pptx", QAPolicy: "ppt-visual", QAEnforcement: "required", DeliveryPolicy: "inbox", CreatedAt: now}
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

// TestCompletionEvaluatorSoftQADoesNotBlock 空/optional QAEnforcement 与不配置等价：不因缺 QA 阻塞。
func TestCompletionEvaluatorSoftQADoesNotBlock(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	store := artifactmemory.NewStore()
	evaluator, err := NewCompletionEvaluator(store, store, store, store, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := artifactmodel.DeliverableSpec{ID: "deck", TenantID: "tenant", RunID: "run", Required: true, Role: artifactmodel.DeliverableRolePrimary, DesiredName: "deck.pptx", QAPolicy: "visual-qa/v1", DeliveryPolicy: "inbox", CreatedAt: now}
	if err := store.CreateDeliverable(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSelection(ctx, "tenant", "run", artifactmodel.DeliverableSelection{DeliverableID: "deck", ProducedResourceID: "produced-1", SelectedBy: "harness", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	publication := artifactmodel.ArtifactPublicationRecord{ID: "publication-1", TenantID: "tenant", RunID: "run", ProducedResourceID: "produced-1", DeliverableID: "deck", DesiredName: "deck.pptx", ArtifactID: "artifact-1", SubjectVersion: "v1", SubjectSHA256: "sha", GateVersion: "gate-v1", IdempotencyKey: "pk", Status: artifactmodel.PublicationCommitted, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, _, err := store.CreatePublication(ctx, publication); err != nil {
		t.Fatal(err)
	}
	delivery := artifactmodel.DeliveryRecord{ID: "delivery-1", TenantID: "tenant", RunID: "run", DeliverableID: "deck", PublicationID: "publication-1", ArtifactID: "artifact-1", Target: workmodel.ResourceRef{Authority: "product", Scheme: "inbox", ID: "target", Scope: workmodel.ResourceScope{TenantID: "tenant"}}, TargetKind: artifactmodel.DeliveryProductInbox, TargetName: "deck.pptx", ResultResource: workmodel.ResourceRef{Authority: "product", Scheme: "delivery", ID: "result", Scope: workmodel.ResourceScope{TenantID: "tenant"}}, IdempotencyKey: "dk", Status: artifactmodel.DeliverySucceeded, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, _, err := store.CreateDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	got, err := evaluator.EvaluateCompletion(ctx, "tenant", "run")
	if err != nil || !got.Complete {
		t.Fatalf("soft QA must not block: got=%+v err=%v", got, err)
	}
}

type stubAdoptions []artifactcontract.AdoptionRecord

func (s stubAdoptions) ListByConsumer(string, string) []artifactcontract.AdoptionRecord {
	return append([]artifactcontract.AdoptionRecord(nil), s...)
}

// TestCompletionEvaluatorAcceptsAdoptedChildDelivery 父未本地 select，但已接纳且子已交付匹配类型 → 销账。
func TestCompletionEvaluatorAcceptsAdoptedChildDelivery(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	store := artifactmemory.NewStore()
	evaluator, err := NewCompletionEvaluator(store, store, store, store, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	evaluator.WithAdoptions(stubAdoptions{{
		ConsumerTenantID: "tenant", ConsumerRunID: "parent",
		ProducedID: "produced-child", OwnerTenantID: "tenant", OwnerRunID: "child",
	}})
	parentSpec := artifactmodel.DeliverableSpec{ID: "parent-deck", TenantID: "tenant", RunID: "parent", Required: true, Role: artifactmodel.DeliverableRolePrimary, AcceptedSuffix: []string{".pptx"}, AcceptedMIMEs: []string{"application/vnd.openxmlformats-officedocument.presentationml.presentation"}, DeliveryPolicy: "run-output", CreatedAt: now}
	if err := store.CreateDeliverable(ctx, parentSpec); err != nil {
		t.Fatal(err)
	}
	childSpec := artifactmodel.DeliverableSpec{ID: "child-deck", TenantID: "tenant", RunID: "child", Required: true, Role: artifactmodel.DeliverableRolePrimary, DesiredName: "deck.pptx", AcceptedSuffix: []string{".pptx"}, DeliveryPolicy: "run-output", CreatedAt: now}
	if err := store.CreateDeliverable(ctx, childSpec); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSelection(ctx, "tenant", "child", artifactmodel.DeliverableSelection{DeliverableID: "child-deck", ProducedResourceID: "produced-child", SelectedBy: "harness", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	publication := artifactmodel.ArtifactPublicationRecord{ID: "pub-child", TenantID: "tenant", RunID: "child", ProducedResourceID: "produced-child", DeliverableID: "child-deck", DesiredName: "deck.pptx", ArtifactID: "art-child", SubjectVersion: "v1", SubjectSHA256: "sha", GateVersion: "g", IdempotencyKey: "pk", Status: artifactmodel.PublicationCommitted, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, _, err := store.CreatePublication(ctx, publication); err != nil {
		t.Fatal(err)
	}
	delivery := artifactmodel.DeliveryRecord{ID: "del-child", TenantID: "tenant", RunID: "child", DeliverableID: "child-deck", PublicationID: "pub-child", ArtifactID: "art-child", Target: workmodel.ResourceRef{Authority: "host", Scheme: "delivery-root", ID: "root", Scope: workmodel.ResourceScope{TenantID: "tenant"}}, TargetKind: artifactmodel.DeliveryProjectRoot, TargetName: "deck.pptx", ResultResource: workmodel.ResourceRef{Authority: "host", Scheme: "delivery", ID: "r", Scope: workmodel.ResourceScope{TenantID: "tenant"}}, IdempotencyKey: "dk", Status: artifactmodel.DeliverySucceeded, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, _, err := store.CreateDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	got, err := evaluator.EvaluateCompletion(ctx, "tenant", "parent")
	if err != nil || !got.Complete {
		t.Fatalf("adopted child delivery should complete parent: got=%+v err=%v", got, err)
	}
}

func TestCompletionEvaluatorIgnoresOptionalDeliverables(t *testing.T) {
	store := artifactmemory.NewStore()
	now := time.Now().UTC()
	if err := store.CreateDeliverable(context.Background(), artifactmodel.DeliverableSpec{ID: "thumb", TenantID: "tenant", RunID: "run", Required: false, Role: artifactmodel.DeliverableRoleSupporting, DeliveryPolicy: "none", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	evaluator, _ := NewCompletionEvaluator(store, store, store, store, store, nil)
	got, err := evaluator.EvaluateCompletion(context.Background(), "tenant", "run")
	if err != nil || !got.Complete {
		t.Fatalf("optional deliverable must not block: got=%+v err=%v", got, err)
	}
}

func TestCompletionEvaluatorBindsSpecFromProducedEvidence(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	store := artifactmemory.NewStore()
	produced := workmemory.NewProducedResourceStore()
	if err := produced.Create(ctx, workmodel.ProducedResourceDescriptor{
		ID: "p1", TenantID: "tenant", RunID: "run", BindingID: "b1", LogicalRef: "run:/work/b1/deck.pptx",
		Source:       workmodel.ResourceRef{Authority: "host", Scheme: "run-file", ID: "loc-p1", Version: "sha256:abc", MediaType: "application/pptx", Scope: workmodel.ResourceScope{TenantID: "tenant"}},
		ObservedName: "deck.pptx", MediaType: "application/pptx", Size: 3, Availability: workmodel.ResourceAvailabilityDurable, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	evaluator, err := NewCompletionEvaluator(store, store, store, store, store, produced)
	if err != nil {
		t.Fatal(err)
	}
	got, err := evaluator.EvaluateCompletion(ctx, "tenant", "run")
	if err != nil {
		t.Fatal(err)
	}
	if got.Complete || len(got.MissingDeliverableIDs) != 1 {
		t.Fatalf("produced pptx without delivery must block: got=%+v", got)
	}
	specs, err := store.ListDeliverables(ctx, "tenant", "run")
	if err != nil || len(specs) != 1 || !specs[0].Required {
		t.Fatalf("expected evidence-bound primary spec, got %+v err=%v", specs, err)
	}
}
