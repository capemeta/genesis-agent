package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestDeliveryServiceOverwritesOnTargetConflict(t *testing.T) {
	fixture := newDeliveryFixture(t)
	fixture.materializer.occupied = map[string]bool{"result.txt": true}
	result, err := fixture.service.Deliver(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.Name != "result.txt" || fixture.materializer.replaceCalls != 1 {
		t.Fatalf("target=%+v replaceCalls=%d", result.Target, fixture.materializer.replaceCalls)
	}
	records, _ := fixture.control.ListDeliveries(context.Background(), "tenant", "run", "deliverable")
	if len(records) != 1 || records[0].Status != artifactmodel.DeliverySucceeded || records[0].TargetName != "result.txt" {
		t.Fatalf("records=%+v", records)
	}
}

func TestDeliveryServiceReplacesTargetAfterPriorSuccess(t *testing.T) {
	fixture := newDeliveryFixture(t)
	if _, err := fixture.service.Deliver(context.Background(), fixture.request); err != nil {
		t.Fatal(err)
	}
	// 模拟 supersede：新 Artifact / Publication / Selection，同名目标已存在。
	ctx := context.Background()
	now := time.Now().UTC()
	scope := fixture.publication.descriptor.Source.Scope
	if err := fixture.control.ReplaceSelection(ctx, "tenant", "run", artifactmodel.DeliverableSelection{DeliverableID: "deliverable", ProducedResourceID: "produced-2", SelectedBy: "harness-supersede", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	pub := artifactmodel.ArtifactPublicationRecord{
		ID: "publication-new", TenantID: "tenant", RunID: "run", ProducedResourceID: "produced-2", DeliverableID: "deliverable",
		DesiredName: "result.txt", GateVersion: "basic/v1", IdempotencyKey: "new-key", ArtifactID: "artifact-new",
		SubjectVersion: "v2", SubjectSHA256: "bbbb", Status: artifactmodel.PublicationCommitted, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, _, err := fixture.control.CreatePublication(ctx, pub); err != nil {
		t.Fatal(err)
	}
	fixture.publication.store.mu.Lock()
	fixture.publication.store.committed["artifact-new"] = artifactmodel.ArtifactRef{ID: "artifact-new", Name: "result.txt", SHA256: "bbbb", Scope: scope, StorageRef: workmodel.ResourceRef{Authority: "memory", Scheme: "artifact", ID: "artifact-new", Version: "sha256:bbbb", Scope: scope}}
	fixture.publication.store.mu.Unlock()
	fixture.materializer.conflictOnce = true
	fixture.materializer.result = nil
	result, err := fixture.service.Deliver(ctx, DeliveryRequest{TenantID: "tenant", RunID: "run", DeliverableID: "deliverable"})
	if err != nil {
		t.Fatal(err)
	}
	if fixture.materializer.replaceCalls != 1 || result.Resource.ID == "" {
		t.Fatalf("replaceCalls=%d result=%+v", fixture.materializer.replaceCalls, result)
	}
}

func TestDeliveryServiceRecordsSuccessAndIdempotentReplay(t *testing.T) {
	fixture := newDeliveryFixture(t)
	first, err := fixture.service.Deliver(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.Deliver(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Resource.ID != second.Resource.ID || fixture.materializer.calls != 1 {
		t.Fatalf("first=%+v second=%+v calls=%d", first, second, fixture.materializer.calls)
	}
	records, _ := fixture.control.ListDeliveries(context.Background(), "tenant", "run", "deliverable")
	if len(records) != 1 || records[0].Status != artifactmodel.DeliverySucceeded {
		t.Fatalf("records=%+v", records)
	}
}

func TestDeliveryServiceConcurrentReplayMaterializesOnce(t *testing.T) {
	fixture := newDeliveryFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := fixture.service.Deliver(context.Background(), fixture.request)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if fixture.materializer.calls != 1 {
		t.Fatalf("calls=%d", fixture.materializer.calls)
	}
}

func TestDeliveryFailureRetriesOnlyDelivery(t *testing.T) {
	fixture := newDeliveryFixture(t)
	fixture.materializer.failNext = true
	if _, err := fixture.service.Deliver(context.Background(), fixture.request); err == nil {
		t.Fatal("expected failure")
	}
	result, err := fixture.service.Deliver(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Resource.ID == "" || fixture.materializer.calls != 2 || fixture.publication.store.commits != 1 || fixture.publication.reader.opens != 1 {
		t.Fatalf("result=%+v calls=%d commits=%d opens=%d", result, fixture.materializer.calls, fixture.publication.store.commits, fixture.publication.reader.opens)
	}
}

func TestDeliveryServiceRecoversMaterializedResponseLoss(t *testing.T) {
	fixture := newDeliveryFixture(t)
	fixture.materializer.loseResponse = true
	result, err := fixture.service.Deliver(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Resource.ID == "" || fixture.materializer.calls != 1 {
		t.Fatalf("result=%+v calls=%d", result, fixture.materializer.calls)
	}
}

func TestDeliveryServiceRecoversWhenMaterializedTargetPrecedesLedger(t *testing.T) {
	fixture := newDeliveryFixture(t)
	ledger := &failSucceededDeliveryLedger{DeliveryRecordStore: fixture.control, fail: true}
	fixture.service.deliveries = ledger
	if _, err := fixture.service.Deliver(context.Background(), fixture.request); err == nil {
		t.Fatal("expected injected ledger failure")
	}
	result, err := fixture.service.Deliver(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Resource.ID == "" || fixture.materializer.calls != 1 {
		t.Fatalf("result=%+v calls=%d", result, fixture.materializer.calls)
	}
}

func TestDeliveryServicePersistsArtifactOnlyDelivery(t *testing.T) {
	fixture := newDeliveryFixture(t)
	fixture.planner.target.Kind = artifactmodel.DeliveryArtifactOnly
	fixture.planner.target.Resource = fixture.artifact.StorageRef
	fixture.planner.target.Name = fixture.artifact.Name
	result, err := fixture.service.Deliver(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Resource.ID != fixture.artifact.StorageRef.ID {
		t.Fatalf("result=%+v", result)
	}
	records, _ := fixture.control.ListDeliveries(context.Background(), "tenant", "run", "deliverable")
	if len(records) != 1 || records[0].Status != artifactmodel.DeliverySucceeded {
		t.Fatalf("records=%+v", records)
	}
}

func TestDeliveryServiceRequiredQAGatesExactPublicationVersion(t *testing.T) {
	fixture := newDeliveryFixtureWithQA(t, ValidatorVisualQA, "required")
	_, err := fixture.service.Deliver(context.Background(), fixture.request)
	var artifactErr *artifactcontract.Error
	if !errors.As(err, &artifactErr) || artifactErr.Code != artifactcontract.ErrCodeQARequired {
		t.Fatalf("expected QA_REQUIRED before evidence, got %v", err)
	}
	if fixture.materializer.calls != 0 {
		t.Fatalf("required QA must gate before materialization, calls=%d", fixture.materializer.calls)
	}

	publication := currentPublication(t, fixture)
	degraded := exactQAEvidence(publication, artifactmodel.QAEvidenceDegraded)
	degraded.ID = "qa-degraded"
	degraded.FailureCode = "vision_unavailable"
	if err := fixture.control.CreateQAEvidence(context.Background(), degraded); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Deliver(context.Background(), fixture.request); !errors.As(err, &artifactErr) || artifactErr.Code != artifactcontract.ErrCodeQARequired {
		t.Fatalf("degraded evidence must not satisfy required QA, got %v", err)
	}

	passed := exactQAEvidence(publication, artifactmodel.QAEvidencePassed)
	passed.ID = "qa-passed"
	if err := fixture.control.CreateQAEvidence(context.Background(), passed); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Deliver(context.Background(), fixture.request); err != nil {
		t.Fatalf("exact passed evidence should release delivery: %v", err)
	}
	if fixture.materializer.calls != 1 {
		t.Fatalf("delivery should materialize exactly once, calls=%d", fixture.materializer.calls)
	}
}

func TestDeliveryServiceOptionalQARequiresExplicitDegradedEvidence(t *testing.T) {
	fixture := newDeliveryFixtureWithQA(t, ValidatorVisualQA, "optional")
	_, err := fixture.service.Deliver(context.Background(), fixture.request)
	var artifactErr *artifactcontract.Error
	if !errors.As(err, &artifactErr) || artifactErr.Code != artifactcontract.ErrCodeQARequired {
		t.Fatalf("missing optional QA evidence must still block bypass delivery, got %v", err)
	}
	publication := currentPublication(t, fixture)
	degraded := exactQAEvidence(publication, artifactmodel.QAEvidenceDegraded)
	degraded.ID = "qa-optional-degraded"
	degraded.FailureCode = "vision_unavailable"
	if err := fixture.control.CreateQAEvidence(context.Background(), degraded); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Deliver(context.Background(), fixture.request); err != nil {
		t.Fatalf("optional QA should accept exact degraded evidence: %v", err)
	}
}

func TestDeliveryServiceOptionalQAAcceptsExactFailedEvidence(t *testing.T) {
	fixture := newDeliveryFixtureWithQA(t, ValidatorVisualQA, "optional")
	publication := currentPublication(t, fixture)
	failed := exactQAEvidence(publication, artifactmodel.QAEvidenceFailed)
	failed.ID = "qa-optional-failed"
	failed.FailureCode = "visual_qa_failed"
	if err := fixture.control.CreateQAEvidence(context.Background(), failed); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Deliver(context.Background(), fixture.request); err != nil {
		t.Fatalf("optional QA should disclose but not block exact failed evidence: %v", err)
	}
}

type deliveryFixture struct {
	service *DeliveryService
	request DeliveryRequest
	control interface {
		artifactcontract.DeliverableSpecStore
		artifactcontract.DeliverableSelectionStore
		artifactcontract.ArtifactPublicationStore
		artifactcontract.QAEvidenceStore
		artifactcontract.DeliveryRecordStore
	}
	materializer *testRecoverableMaterializer
	planner      *staticPlanner
	publication  publicationFixture
	artifact     artifactmodel.ArtifactRef
}

func newDeliveryFixture(t *testing.T) deliveryFixture {
	return newDeliveryFixtureWithQA(t, "", "")
}

func newDeliveryFixtureWithQA(t *testing.T, qaPolicy, qaEnforcement string) deliveryFixture {
	t.Helper()
	publication := newPublicationFixtureWithQA(t, qaPolicy, qaEnforcement)
	artifact, err := publication.service.Publish(context.Background(), publication.request)
	if err != nil {
		t.Fatal(err)
	}
	planner := &staticPlanner{target: artifactmodel.DeliveryTarget{Kind: artifactmodel.DeliveryProductInbox, Resource: workmodel.ResourceRef{Authority: "product", Scheme: "inbox", ID: "target", Scope: artifact.Scope}, Name: "result.txt"}}
	materializer := &testRecoverableMaterializer{}
	service, err := NewDeliveryService(publication.control, publication.control, publication.control, publication.control, publication.control, publication.store, planner, materializer)
	if err != nil {
		t.Fatal(err)
	}
	return deliveryFixture{service: service, request: DeliveryRequest{TenantID: "tenant", RunID: "run", DeliverableID: "deliverable"}, control: publication.control, materializer: materializer, planner: planner, publication: publication, artifact: artifact}
}

func currentPublication(t *testing.T, fixture deliveryFixture) artifactmodel.ArtifactPublicationRecord {
	t.Helper()
	records, err := fixture.control.ListPublications(context.Background(), "tenant", "run", "deliverable")
	if err != nil || len(records) != 1 {
		t.Fatalf("publications=%+v err=%v", records, err)
	}
	return records[0]
}

func exactQAEvidence(publication artifactmodel.ArtifactPublicationRecord, status artifactmodel.QAEvidenceStatus) artifactmodel.QAEvidenceRecord {
	return artifactmodel.QAEvidenceRecord{
		TenantID: "tenant", RunID: "run", DeliverableID: "deliverable", ProducedResourceID: publication.ProducedResourceID,
		PublicationID: publication.ID, SubjectVersion: publication.SubjectVersion, SubjectSHA256: publication.SubjectSHA256,
		PolicyID: ValidatorVisualQA, Validator: ValidatorVisualQA, ValidatorVersion: "visual-checklist/v1", Status: status, CreatedAt: time.Now().UTC(),
	}
}

type staticPlanner struct{ target artifactmodel.DeliveryTarget }

func (p *staticPlanner) PlanDelivery(context.Context, artifactmodel.DeliverableSpec, artifactmodel.ArtifactRef) (artifactmodel.DeliveryTarget, error) {
	return p.target, nil
}

type testRecoverableMaterializer struct {
	mu           sync.Mutex
	calls        int
	replaceCalls int
	failNext     bool
	loseResponse bool
	conflictOnce bool
	occupied     map[string]bool
	result       *artifactmodel.DeliveryResult
}

type failSucceededDeliveryLedger struct {
	artifactcontract.DeliveryRecordStore
	fail bool
}

func (s *failSucceededDeliveryLedger) UpdateDelivery(ctx context.Context, expected uint64, value artifactmodel.DeliveryRecord) (artifactmodel.DeliveryRecord, error) {
	if s.fail && value.Status == artifactmodel.DeliverySucceeded {
		s.fail = false
		return artifactmodel.DeliveryRecord{}, fmt.Errorf("ledger unavailable")
	}
	return s.DeliveryRecordStore.UpdateDelivery(ctx, expected, value)
}

func (m *testRecoverableMaterializer) Materialize(_ context.Context, artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget) (artifactmodel.DeliveryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.failNext {
		m.failNext = false
		return artifactmodel.DeliveryResult{}, artifactcontract.NewError(artifactcontract.ErrCodeDeliveryMaterializeFailed, fmt.Errorf("temporary"))
	}
	if m.conflictOnce {
		m.conflictOnce = false
		return artifactmodel.DeliveryResult{}, artifactcontract.NewError(artifactcontract.ErrCodeDeliveryTargetConflict, fmt.Errorf("目标已存在: %s", target.Name))
	}
	if m.occupied != nil && m.occupied[target.Name] {
		return artifactmodel.DeliveryResult{}, artifactcontract.NewError(artifactcontract.ErrCodeDeliveryTargetConflict, fmt.Errorf("目标已存在: %s", target.Name))
	}
	return m.successLocked(artifact, target)
}
func (m *testRecoverableMaterializer) GetMaterialized(_ context.Context, _ artifactmodel.ArtifactRef, _ artifactmodel.DeliveryTarget) (artifactmodel.DeliveryResult, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.result == nil {
		return artifactmodel.DeliveryResult{}, false, nil
	}
	return *m.result, true, nil
}

func (m *testRecoverableMaterializer) ReplaceMaterialize(_ context.Context, artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget) (artifactmodel.DeliveryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replaceCalls++
	return m.successLocked(artifact, target)
}

func (m *testRecoverableMaterializer) successLocked(artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget) (artifactmodel.DeliveryResult, error) {
	display := target.Name
	if display == "" {
		display = "result.txt"
	}
	result := artifactmodel.DeliveryResult{Artifact: artifact, Target: target, Resource: workmodel.ResourceRef{Authority: "product", Scheme: "delivery", ID: "delivered", Path: target.Name, Version: "sha256:" + artifact.SHA256, Scope: artifact.Scope}, Display: display}
	if target.Kind == artifactmodel.DeliveryArtifactOnly {
		result.Resource = artifact.StorageRef
		result.Display = ""
	}
	if m.occupied == nil {
		m.occupied = map[string]bool{}
	}
	m.occupied[target.Name] = true
	m.result = &result
	if m.loseResponse {
		m.loseResponse = false
		return artifactmodel.DeliveryResult{}, fmt.Errorf("response lost")
	}
	return result, nil
}
