package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	artifactmemory "genesis-agent/internal/capabilities/artifact/adapter/memory"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmemory "genesis-agent/internal/capabilities/workspace/adapter/memory"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestArtifactPublicationServicePublishesAndReplaysWithoutReopeningSource(t *testing.T) {
	fixture := newPublicationFixture(t)
	first, err := fixture.service.Publish(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.Publish(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.SHA256 != second.SHA256 || fixture.reader.opens != 1 {
		t.Fatalf("first=%+v second=%+v opens=%d", first, second, fixture.reader.opens)
	}
	records, _ := fixture.control.ListPublications(context.Background(), "tenant", "run", "deliverable")
	if len(records) != 1 || records[0].Status != artifactmodel.PublicationCommitted || records[0].SubjectVersion != "v1" {
		t.Fatalf("records=%+v", records)
	}
}

func TestArtifactPublicationServiceRecoversCommitBeforeLedgerUpdate(t *testing.T) {
	fixture := newPublicationFixture(t)
	fixture.store.failAfterCommit = true
	artifact, err := fixture.service.Publish(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.ID == "" || fixture.store.commits != 1 {
		t.Fatalf("artifact=%+v commits=%d", artifact, fixture.store.commits)
	}
	records, _ := fixture.control.ListPublications(context.Background(), "tenant", "run", "deliverable")
	if len(records) != 1 || records[0].Status != artifactmodel.PublicationCommitted {
		t.Fatalf("records=%+v", records)
	}
}

func TestArtifactPublicationServiceRecoversWhenCommittedArtifactPrecedesLedger(t *testing.T) {
	fixture := newPublicationFixture(t)
	ledger := &failCommitLedgerStore{Store: fixture.control, fail: true}
	fixture.service.publications = ledger
	if _, err := fixture.service.Publish(context.Background(), fixture.request); err == nil {
		t.Fatal("expected injected ledger failure")
	}
	artifact, err := fixture.service.Publish(context.Background(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.ID == "" || fixture.store.commits != 1 {
		t.Fatalf("artifact=%+v commits=%d", artifact, fixture.store.commits)
	}
}

func TestArtifactPublicationServiceConcurrentReplayCommitsOnce(t *testing.T) {
	fixture := newPublicationFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := fixture.service.Publish(context.Background(), fixture.request)
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
	if fixture.store.commits != 1 {
		t.Fatalf("commits=%d", fixture.store.commits)
	}
}

func TestArtifactPublicationServiceRejectsExpiredDescriptorBeforeReading(t *testing.T) {
	fixture := newPublicationFixture(t)
	expired := fixture.descriptor
	expiry := time.Now().Add(-time.Minute)
	expired.Availability = workmodel.ResourceAvailabilityLeased
	expired.ExpiresAt = &expiry
	produced := workmemory.NewProducedResourceStore()
	if err := produced.Create(context.Background(), expired); err != nil {
		t.Fatal(err)
	}
	fixture.service.produced = produced
	_, err := fixture.service.Publish(context.Background(), fixture.request)
	var classified *workcontract.Error
	if !errors.As(err, &classified) || classified.Code != workcontract.ErrCodeProducedResourceExpired || fixture.reader.opens != 0 {
		t.Fatalf("err=%v opens=%d", err, fixture.reader.opens)
	}
}

func TestArtifactPublicationServicePersistsGateFailureCodeAndReturnsClassifiedError(t *testing.T) {
	fixture := newPublicationFixture(t)
	fixture.service.gate = rejectingGate{}
	_, err := fixture.service.Publish(context.Background(), fixture.request)
	var classified *artifactcontract.Error
	if !errors.As(err, &classified) || classified.Code != artifactcontract.ErrCodeArtifactInvalid {
		t.Fatalf("err=%v", err)
	}
	if classified.Validator != "rejecting_gate" || classified.Reason != "test_reject" {
		t.Fatalf("error detail validator=%q reason=%q", classified.Validator, classified.Reason)
	}
	records, _ := fixture.control.ListPublications(context.Background(), "tenant", "run", "deliverable")
	if len(records) != 1 || records[0].Status != artifactmodel.PublicationFailed || records[0].FailureCode != string(artifactcontract.ErrCodeArtifactInvalid) {
		t.Fatalf("records=%+v", records)
	}
	if records[0].FailureValidator != "rejecting_gate" || records[0].FailureReason != "test_reject" {
		t.Fatalf("persisted gate detail=%+v", records[0])
	}
}

func TestArtifactPublicationServiceRejectsIncompleteRequestAsArtifactInvalid(t *testing.T) {
	fixture := newPublicationFixture(t)
	_, err := fixture.service.Publish(context.Background(), PublicationRequest{})
	var classified *artifactcontract.Error
	if !errors.As(err, &classified) || classified.Code != artifactcontract.ErrCodeArtifactInvalid {
		t.Fatalf("err=%v", err)
	}
}

func TestGatePipelineReturnsStructuredValidatorReason(t *testing.T) {
	_, _, err := DefaultGatePipeline().Validate(context.Background(), "empty.txt", "text/plain", 0, bytes.NewReader(nil))
	var classified *artifactcontract.Error
	if !errors.As(err, &classified) || classified.Code != artifactcontract.ErrCodeArtifactInvalid {
		t.Fatalf("err=%v", err)
	}
	if classified.Validator != "size_limit" || classified.Reason != "empty_artifact" {
		t.Fatalf("validator=%q reason=%q", classified.Validator, classified.Reason)
	}
}

type publicationFixture struct {
	service    *ArtifactPublicationService
	request    PublicationRequest
	control    *artifactmemory.Store
	reader     *testPublicationReader
	store      *testTransactionalStore
	descriptor workmodel.ProducedResourceDescriptor
}

func newPublicationFixture(t *testing.T) publicationFixture {
	return newPublicationFixtureWithQA(t, "", "")
}

func newPublicationFixtureWithQA(t *testing.T, qaPolicy, qaEnforcement string) publicationFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	scope := workmodel.ResourceScope{TenantID: "tenant"}
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", RunID: "run", AgentAppID: "app", AgentAppVersion: "1"}}
	profile := agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	manifest := workmodel.RunManifest{SchemaVersion: workmodel.RunManifestSchemaVersion, Revision: 1, RunID: "run", Scope: scope, AgentApp: profile, StateRoot: workmodel.StateRoot{ID: "state", Authority: "executor", Scope: scope}, Limits: workmodel.WorkspaceLimits{MaximumAccess: execmodel.WorkspaceAccessReadWrite}, Executions: []workmodel.PreparedExecutionSnapshot{{Binding: binding, Backend: execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindRemote, Authority: "executor"}, Workspace: execmodel.ExecutionWorkspace{WorkDir: "/workspace/work", InputDir: "/workspace/input", OutputDir: "/workspace/output", TmpDir: "/workspace/tmp"}}}, Inputs: workmodel.InputManifest{RunID: "run", BindingID: "binding"}, View: workmodel.WorkspaceViewManifest{BindingID: "binding", Root: "."}, CreatedAt: now}
	manifests := workmemory.NewManifestStore()
	if err := manifests.Create(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	content := []byte("hello publication")
	descriptor := workmodel.ProducedResourceDescriptor{ID: "produced", TenantID: "tenant", RunID: "run", BindingID: "binding", LogicalRef: "run:/work/binding/output.txt", Source: workmodel.ResourceRef{Authority: "executor", Scheme: "session-file", ID: "opaque", Version: "v1", MediaType: "text/plain", Scope: scope}, ObservedName: "output.txt", MediaType: "text/plain", Size: int64(len(content)), Availability: workmodel.ResourceAvailabilityDurable, CreatedAt: now}
	produced := workmemory.NewProducedResourceStore()
	if err := produced.Create(ctx, descriptor); err != nil {
		t.Fatal(err)
	}
	control := artifactmemory.NewStore()
	spec := artifactmodel.DeliverableSpec{ID: "deliverable", TenantID: "tenant", RunID: "run", Required: true, Role: artifactmodel.DeliverableRolePrimary, DesiredName: "result.txt", AcceptedMIMEs: []string{"text/plain"}, AcceptedSuffix: []string{".txt"}, QAPolicy: qaPolicy, QAEnforcement: qaEnforcement, DeliveryPolicy: "download", CreatedAt: now}
	if err := control.CreateDeliverable(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if err := control.CreateSelection(ctx, "tenant", "run", artifactmodel.DeliverableSelection{DeliverableID: "deliverable", ProducedResourceID: "produced", SelectedBy: "harness", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	reader := &testPublicationReader{content: content}
	store := newTestTransactionalStore()
	service, err := NewArtifactPublicationService(control, control, control, produced, manifests, reader, store, BasicGate{})
	if err != nil {
		t.Fatal(err)
	}
	return publicationFixture{service: service, request: PublicationRequest{TenantID: "tenant", RunID: "run", DeliverableID: "deliverable", ProducedResourceID: "produced"}, control: control, reader: reader, store: store, descriptor: descriptor}
}

type testPublicationReader struct {
	content []byte
	opens   int
}

type rejectingGate struct{}

func (rejectingGate) Version() string { return "reject/v1" }
func (rejectingGate) Validate(context.Context, string, string, int64, io.Reader) (string, string, error) {
	return "", "", artifactcontract.NewGateError("rejecting_gate", "test_reject", fmt.Errorf("rejected"))
}

type failCommitLedgerStore struct {
	*artifactmemory.Store
	fail bool
}

func (s *failCommitLedgerStore) UpdatePublication(ctx context.Context, expected uint64, value artifactmodel.ArtifactPublicationRecord) (artifactmodel.ArtifactPublicationRecord, error) {
	if s.fail && value.Status == artifactmodel.PublicationCommitted {
		s.fail = false
		return artifactmodel.ArtifactPublicationRecord{}, fmt.Errorf("ledger unavailable")
	}
	return s.Store.UpdatePublication(ctx, expected, value)
}

func (r *testPublicationReader) Open(_ context.Context, _ execmodel.ExecutionBackendRef, source workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	r.opens++
	return workcontract.ResourceHandle{Reader: io.NopCloser(bytes.NewReader(r.content)), Size: int64(len(r.content)), Version: source.Version, MediaType: source.MediaType}, nil
}

type readerCloser struct{ *bytes.Reader }

func (readerCloser) Close() error { return nil }

type testTransactionalStore struct {
	mu              sync.Mutex
	staged          map[string][]byte
	committed       map[string]artifactmodel.ArtifactRef
	commits         int
	stages          int
	failAfterCommit bool
}

func newTestTransactionalStore() *testTransactionalStore {
	return &testTransactionalStore{staged: map[string][]byte{}, committed: map[string]artifactmodel.ArtifactRef{}}
}
func (s *testTransactionalStore) Stage(_ context.Context, id, name string, content io.Reader) (artifactcontract.StagedObject, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return artifactcontract.StagedObject{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stages++
	key := fmt.Sprintf("%s-stage-%d", id, s.stages)
	s.staged[key] = data
	return artifactcontract.StagedObject{ID: id, Name: key}, nil
}
func (s *testTransactionalStore) OpenStaged(_ context.Context, obj artifactcontract.StagedObject) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.staged[obj.Name]
	if !ok {
		return nil, fmt.Errorf("staged missing")
	}
	return readerCloser{bytes.NewReader(append([]byte(nil), data...))}, nil
}
func (s *testTransactionalStore) Commit(_ context.Context, obj artifactcontract.StagedObject, manifest artifactmodel.Manifest) (artifactmodel.ArtifactRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.staged[obj.Name]; !ok {
		return artifactmodel.ArtifactRef{}, fmt.Errorf("staged missing")
	}
	manifest.StorageRef = workmodel.ResourceRef{Authority: "memory", Scheme: "artifact", ID: manifest.ID, Version: "sha256:" + manifest.SHA256, Scope: manifest.Scope}
	s.committed[manifest.ID] = manifest.ArtifactRef
	delete(s.staged, obj.Name)
	s.commits++
	if s.failAfterCommit {
		s.failAfterCommit = false
		return artifactmodel.ArtifactRef{}, fmt.Errorf("response lost")
	}
	return manifest.ArtifactRef, nil
}
func (s *testTransactionalStore) Abort(_ context.Context, obj artifactcontract.StagedObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.staged, obj.Name)
	return nil
}
func (s *testTransactionalStore) Open(_ context.Context, artifact artifactmodel.ArtifactRef) (io.ReadCloser, error) {
	return nil, fmt.Errorf("unused %s", artifact.ID)
}
func (s *testTransactionalStore) GetCommitted(_ context.Context, id string) (artifactmodel.ArtifactRef, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.committed[id]
	return v, ok, nil
}
