package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	artifactmemory "genesis-agent/internal/capabilities/artifact/adapter/memory"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmemory "genesis-agent/internal/capabilities/workspace/adapter/memory"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type recordingPublisher struct{ calls []PublicationRequest }

func (p *recordingPublisher) Publish(_ context.Context, req PublicationRequest) (artifactmodel.ArtifactRef, error) {
	p.calls = append(p.calls, req)
	return artifactmodel.ArtifactRef{ID: "artifact", Name: "deck.pptx"}, nil
}

type recordingDelivery struct{ calls []DeliveryRequest }

func (d *recordingDelivery) Deliver(_ context.Context, req DeliveryRequest) (artifactmodel.DeliveryResult, error) {
	d.calls = append(d.calls, req)
	return artifactmodel.DeliveryResult{Display: "deck.pptx", Target: artifactmodel.DeliveryTarget{Name: "deck.pptx"}}, nil
}

type conflictDelivery struct{ calls int }

func (d *conflictDelivery) Deliver(context.Context, DeliveryRequest) (artifactmodel.DeliveryResult, error) {
	d.calls++
	return artifactmodel.DeliveryResult{}, artifactcontract.NewError(artifactcontract.ErrCodeDeliveryTargetConflict, fmt.Errorf("目标已存在: deck.pptx"))
}

func TestDeterministicFinalizerOnlyAutoSelectsUniqueMatch(t *testing.T) {
	ctx := context.Background()
	ledger := artifactmemory.NewStore()
	produced := workmemory.NewProducedResourceStore()
	now := time.Now().UTC()
	spec := artifactmodel.DeliverableSpec{ID: "d1", TenantID: "tenant", RunID: "run", Required: true, Role: artifactmodel.DeliverableRolePrimary, DesiredName: "deck.pptx", AcceptedMIMEs: []string{"application/pptx"}, AcceptedSuffix: []string{".pptx"}, DeliveryPolicy: "out", CreatedAt: now}
	if err := ledger.CreateDeliverable(ctx, spec); err != nil {
		t.Fatal(err)
	}
	createProduced(t, produced, "p1", "run:/work/b1/deck.pptx", "deck.pptx", "application/pptx", now)
	pub, delivery := &recordingPublisher{}, &recordingDelivery{}
	finalizer, _ := NewDeterministicFinalizer(ledger, ledger, produced, pub, ledger, ledger, delivery)
	result, err := finalizer.FinalizeRequired(ctx, "tenant", "run")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resolutions) != 1 || result.Resolutions[0].Status != "delivered" || result.Resolutions[0].SelectedID != "p1" {
		t.Fatalf("result=%+v", result)
	}
	selection, err := ledger.GetSelection(ctx, "tenant", "run", "d1")
	if err != nil || selection.ProducedResourceID != "p1" {
		t.Fatalf("selection=%+v err=%v", selection, err)
	}
	if len(pub.calls) != 1 || len(delivery.calls) != 1 {
		t.Fatalf("publish=%d delivery=%d", len(pub.calls), len(delivery.calls))
	}
}

func TestDeterministicFinalizerReturnsOpaqueIDsForMultipleMatches(t *testing.T) {
	ctx := context.Background()
	ledger := artifactmemory.NewStore()
	produced := workmemory.NewProducedResourceStore()
	now := time.Now().UTC()
	_ = ledger.CreateDeliverable(ctx, artifactmodel.DeliverableSpec{ID: "d1", TenantID: "tenant", RunID: "run", Required: true, Role: artifactmodel.DeliverableRolePrimary, AcceptedMIMEs: []string{"application/pptx"}, AcceptedSuffix: []string{".pptx"}, DeliveryPolicy: "out", CreatedAt: now})
	createProduced(t, produced, "p2", "run:/work/b1/b.pptx", "b.pptx", "application/pptx", now)
	createProduced(t, produced, "p1", "run:/work/b1/a.pptx", "a.pptx", "application/pptx", now)
	pub, delivery := &recordingPublisher{}, &recordingDelivery{}
	finalizer, _ := NewDeterministicFinalizer(ledger, ledger, produced, pub, ledger, ledger, delivery)
	result, err := finalizer.FinalizeRequired(ctx, "tenant", "run")
	if err != nil {
		t.Fatal(err)
	}
	got := result.Resolutions[0]
	if got.Status != "selection_required" || len(got.CandidateIDs) != 2 || got.CandidateIDs[0] != "p1" || len(pub.calls) != 0 {
		t.Fatalf("result=%+v", result)
	}
	if _, err := finalizer.SelectAndFinalize(ctx, "tenant", "run", "d1", "p2"); err != nil {
		t.Fatal(err)
	}
	if len(pub.calls) != 1 || pub.calls[0].ProducedResourceID != "p2" {
		t.Fatalf("publish=%+v", pub.calls)
	}
}

func TestDeterministicFinalizerRebindsSelectionAfterSupersede(t *testing.T) {
	ctx := context.Background()
	ledger := artifactmemory.NewStore()
	produced := workmemory.NewProducedResourceStore()
	now := time.Now().UTC()
	spec := artifactmodel.DeliverableSpec{ID: "d1", TenantID: "tenant", RunID: "run", Required: true, Role: artifactmodel.DeliverableRolePrimary, DesiredName: "deck.pptx", AcceptedMIMEs: []string{"application/pptx"}, AcceptedSuffix: []string{".pptx"}, DeliveryPolicy: "out", CreatedAt: now}
	if err := ledger.CreateDeliverable(ctx, spec); err != nil {
		t.Fatal(err)
	}
	createProduced(t, produced, "p1", "run:/work/b1/deck.pptx", "deck.pptx", "application/pptx", now)
	pub, delivery := &recordingPublisher{}, &recordingDelivery{}
	finalizer, _ := NewDeterministicFinalizer(ledger, ledger, produced, pub, ledger, ledger, delivery)
	if _, err := finalizer.FinalizeRequired(ctx, "tenant", "run"); err != nil {
		t.Fatal(err)
	}
	if err := produced.UpsertCurrent(context.Background(), workmodel.ProducedResourceDescriptor{
		ID: "p2", TenantID: "tenant", RunID: "run", BindingID: "b1", LogicalRef: "run:/work/b1/deck.pptx",
		Source:       workmodel.ResourceRef{Authority: "host", Scheme: "run-file", ID: "loc-p2", Version: "sha256:def", MediaType: "application/pptx", Scope: workmodel.ResourceScope{TenantID: "tenant"}},
		ObservedName: "deck.pptx", MediaType: "application/pptx", Size: 4, Availability: workmodel.ResourceAvailabilityDurable, CreatedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	result, err := finalizer.FinalizeRequired(ctx, "tenant", "run")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resolutions) != 1 || result.Resolutions[0].Status != "delivered" || result.Resolutions[0].SelectedID != "p2" {
		t.Fatalf("result=%+v", result)
	}
	selection, err := ledger.GetSelection(ctx, "tenant", "run", "d1")
	if err != nil || selection.ProducedResourceID != "p2" {
		t.Fatalf("selection=%+v err=%v", selection, err)
	}
	if len(pub.calls) != 2 || pub.calls[1].ProducedResourceID != "p2" {
		t.Fatalf("publish calls=%+v", pub.calls)
	}
}

func TestDeterministicFinalizerSoftensDeliveryTargetConflict(t *testing.T) {
	ctx := context.Background()
	ledger := artifactmemory.NewStore()
	produced := workmemory.NewProducedResourceStore()
	now := time.Now().UTC()
	spec := artifactmodel.DeliverableSpec{ID: "d1", TenantID: "tenant", RunID: "run", Required: true, Role: artifactmodel.DeliverableRolePrimary, DesiredName: "deck.pptx", AcceptedMIMEs: []string{"application/pptx"}, AcceptedSuffix: []string{".pptx"}, DeliveryPolicy: "out", CreatedAt: now}
	if err := ledger.CreateDeliverable(ctx, spec); err != nil {
		t.Fatal(err)
	}
	createProduced(t, produced, "p1", "run:/work/b1/deck.pptx", "deck.pptx", "application/pptx", now)
	pub, delivery := &recordingPublisher{}, &conflictDelivery{}
	finalizer, _ := NewDeterministicFinalizer(ledger, ledger, produced, pub, ledger, ledger, delivery)
	result, err := finalizer.FinalizeRequired(ctx, "tenant", "run")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resolutions) != 1 || result.Resolutions[0].Status != "delivery_conflict" || result.Resolutions[0].Warning == "" {
		t.Fatalf("result=%+v", result)
	}
	if delivery.calls != 1 || len(pub.calls) != 1 {
		t.Fatalf("publish=%d delivery=%d", len(pub.calls), delivery.calls)
	}
}

func TestDeterministicFinalizerBindsSpecFromProducedEvidence(t *testing.T) {
	ctx := context.Background()
	ledger := artifactmemory.NewStore()
	produced := workmemory.NewProducedResourceStore()
	now := time.Now().UTC()
	createProduced(t, produced, "p1", "run:/work/b1/deck.pptx", "deck.pptx", "application/pptx", now)
	createProduced(t, produced, "qa1", "run:/work/b1/slide-1.jpg", "slide-1.jpg", "image/jpeg", now)
	pub, delivery := &recordingPublisher{}, &recordingDelivery{}
	finalizer, _ := NewDeterministicFinalizer(ledger, ledger, produced, pub, ledger, ledger, delivery)
	ctx = artifactcontract.WithEvidenceQAHints(ctx, artifactcontract.EvidenceQAHints{Policy: "visual-qa/v1", Enforcement: "optional"})
	result, err := finalizer.FinalizeRequired(ctx, "tenant", "run")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resolutions) != 1 || result.Resolutions[0].Status != "qa_pending" || result.Resolutions[0].SelectedID != "p1" {
		t.Fatalf("result=%+v", result)
	}
	specs, err := ledger.ListDeliverables(ctx, "tenant", "run")
	if err != nil || len(specs) != 1 {
		t.Fatalf("specs=%+v err=%v", specs, err)
	}
	if specs[0].QAPolicy != "visual-qa/v1" || specs[0].QAEnforcement != "optional" {
		t.Fatalf("qa hints not applied: %+v", specs[0])
	}
	if len(pub.calls) != 1 || len(delivery.calls) != 0 {
		t.Fatalf("publish=%d delivery=%d", len(pub.calls), len(delivery.calls))
	}
}

func TestDeterministicFinalizerSkipsEvidenceWhenNoDeliverableProduced(t *testing.T) {
	ctx := context.Background()
	ledger := artifactmemory.NewStore()
	produced := workmemory.NewProducedResourceStore()
	now := time.Now().UTC()
	createProduced(t, produced, "qa1", "run:/work/b1/slide-1.jpg", "slide-1.jpg", "image/jpeg", now)
	pub, delivery := &recordingPublisher{}, &recordingDelivery{}
	finalizer, _ := NewDeterministicFinalizer(ledger, ledger, produced, pub, ledger, ledger, delivery)
	result, err := finalizer.FinalizeRequired(ctx, "tenant", "run")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resolutions) != 0 || len(pub.calls) != 0 {
		t.Fatalf("read-only/qa-only must not create or deliver, result=%+v publish=%d", result, len(pub.calls))
	}
	specs, err := ledger.ListDeliverables(ctx, "tenant", "run")
	if err != nil || len(specs) != 0 {
		t.Fatalf("expected no specs, got %+v err=%v", specs, err)
	}
}

func createProduced(t *testing.T, store *workmemory.ProducedResourceStore, id, logical, name, media string, now time.Time) {
	t.Helper()
	descriptor := workmodel.ProducedResourceDescriptor{ID: id, TenantID: "tenant", RunID: "run", BindingID: "b1", LogicalRef: logical, Source: workmodel.ResourceRef{Authority: "host", Scheme: "run-file", ID: "loc-" + id, Version: "sha256:abc", MediaType: media, Scope: workmodel.ResourceScope{TenantID: "tenant"}}, ObservedName: name, MediaType: media, Size: 3, Availability: workmodel.ResourceAvailabilityDurable, CreatedAt: now}
	if err := store.Create(context.Background(), descriptor); err != nil {
		t.Fatal(err)
	}
}

var _ artifactcontract.RequiredDeliverableFinalizer = (*DeterministicFinalizer)(nil)
