package service

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmemory "genesis-agent/internal/capabilities/workspace/adapter/memory"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type producedResolver struct {
	request workcontract.BackendResourceRequest
	source  workmodel.ResourceRef
}

func (r *producedResolver) ResolveProducedResource(_ context.Context, req workcontract.BackendResourceRequest) (workmodel.ResourceRef, error) {
	r.request = req
	if req.PreferSource != nil &&
		req.PreferSource.Version == r.source.Version &&
		req.PreferSource.Authority == r.source.Authority &&
		req.PreferSource.Scheme == r.source.Scheme {
		out := *req.PreferSource
		out.MediaType = r.source.MediaType
		return out, nil
	}
	return r.source, nil
}

func TestProducedResourceRegistrarPersistsTrustedVersionedDescriptor(t *testing.T) {
	manifestStore := workmemory.NewManifestStore()
	manifest := producedTestManifest()
	if err := manifestStore.Create(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	store := workmemory.NewProducedResourceStore()
	resolver := &producedResolver{source: workmodel.ResourceRef{Authority: "executor", Scheme: "session-file", ID: "opaque-locator", Version: "sha256:abc", Scope: manifest.Scope}}
	registrar, err := NewProducedResourceRegistrar(manifestStore, store, resolver, &fixedIDs{values: []string{"one", "two"}})
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().UTC().Add(time.Hour)
	req := workcontract.RegisterProducedResourceRequest{TenantID: "tenant", RunID: "run", BindingID: "binding", LogicalRef: "run:/work/binding/out.pptx", ObservedPath: "work/binding/out.pptx", ObservedName: "out.pptx", Size: 12, Availability: workmodel.ResourceAvailabilityLeased, ExpiresAt: &expires}
	descriptor, err := registrar.RegisterProducedResource(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.ID != "produced-one" || resolver.request.Execution.Backend.Authority != "executor" || resolver.request.ObservedPath != req.ObservedPath {
		t.Fatalf("descriptor/resolver request = %+v / %+v", descriptor, resolver.request)
	}
	stored, err := store.Get(context.Background(), "tenant", "run", descriptor.ID)
	if err != nil || stored.Source.ID != "opaque-locator" {
		t.Fatalf("stored descriptor = %+v, %v", stored, err)
	}
	again, err := registrar.RegisterProducedResource(context.Background(), req)
	if err != nil || again.ID != descriptor.ID {
		t.Fatalf("same-content re-register should be idempotent: %+v, %v", again, err)
	}
	if resolver.request.PreferSource == nil || resolver.request.PreferSource.ID != descriptor.Source.ID {
		t.Fatalf("re-register must prefer existing source locator: %+v", resolver.request.PreferSource)
	}
	if again.Source.ID != descriptor.Source.ID {
		t.Fatalf("idempotent path must reuse locator id: first=%s again=%s", descriptor.Source.ID, again.Source.ID)
	}
}

func TestProducedResourceRegistrarSupersedesChangedContent(t *testing.T) {
	manifestStore := workmemory.NewManifestStore()
	manifest := producedTestManifest()
	if err := manifestStore.Create(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	store := workmemory.NewProducedResourceStore()
	resolver := &producedResolver{source: workmodel.ResourceRef{Authority: "executor", Scheme: "session-file", ID: "opaque-1", Version: "sha256:abc", Scope: manifest.Scope}}
	registrar, err := NewProducedResourceRegistrar(manifestStore, store, resolver, &fixedIDs{values: []string{"one", "two"}})
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().UTC().Add(time.Hour)
	req := workcontract.RegisterProducedResourceRequest{TenantID: "tenant", RunID: "run", BindingID: "binding", LogicalRef: "run:/work/binding/out.pptx", ObservedPath: "work/binding/out.pptx", ObservedName: "out.pptx", Size: 12, Availability: workmodel.ResourceAvailabilityLeased, ExpiresAt: &expires}
	first, err := registrar.RegisterProducedResource(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	resolver.source = workmodel.ResourceRef{Authority: "executor", Scheme: "session-file", ID: "opaque-2", Version: "sha256:def", Scope: manifest.Scope}
	req.Size = 13
	second, err := registrar.RegisterProducedResource(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.ID != "produced-two" {
		t.Fatalf("supersede should create new head: first=%s second=%s", first.ID, second.ID)
	}
	head, err := store.GetByLogicalRef(context.Background(), "tenant", "run", req.LogicalRef)
	if err != nil || head.ID != second.ID {
		t.Fatalf("logical head = %+v, %v", head, err)
	}
	listed, err := store.ListByRun(context.Background(), "tenant", "run")
	if err != nil || len(listed) != 1 || listed[0].ID != second.ID {
		t.Fatalf("ListByRun should only return current head: %+v, %v", listed, err)
	}
	if _, err := store.Get(context.Background(), "tenant", "run", first.ID); err != nil {
		t.Fatalf("superseded descriptor must remain readable by id: %v", err)
	}
}

func TestProducedResourceRegistrarRejectsExpiredLeaseAndBackendMismatch(t *testing.T) {
	manifestStore := workmemory.NewManifestStore()
	manifest := producedTestManifest()
	if err := manifestStore.Create(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	store := workmemory.NewProducedResourceStore()
	resolver := &producedResolver{source: workmodel.ResourceRef{Authority: "host", Scheme: "run-file", ID: "opaque", Version: "v1", Scope: manifest.Scope}}
	registrar, _ := NewProducedResourceRegistrar(manifestStore, store, resolver, &fixedIDs{values: []string{"one"}})
	expired := time.Now().UTC().Add(-time.Minute)
	req := workcontract.RegisterProducedResourceRequest{TenantID: "tenant", RunID: "run", BindingID: "binding", LogicalRef: "run:/work/binding/out.txt", ObservedPath: "work/binding/out.txt", ObservedName: "out.txt", Size: 1, Availability: workmodel.ResourceAvailabilityLeased, ExpiresAt: &expired}
	if _, err := registrar.RegisterProducedResource(context.Background(), req); !workspaceErrorIs(err, workcontract.ErrCodeProducedResourceInvalid) {
		t.Fatalf("expired lease error = %v", err)
	}
	expires := time.Now().UTC().Add(time.Hour)
	req.ExpiresAt = &expires
	if _, err := registrar.RegisterProducedResource(context.Background(), req); !workspaceErrorIs(err, workcontract.ErrCodeProducedResourceBackendMismatch) {
		t.Fatalf("backend mismatch error = %v", err)
	}
}

type staticResourceReader struct {
	version string
	closed  bool
}

func (r *staticResourceReader) Open(context.Context, workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	return workcontract.ResourceHandle{Reader: &trackingCloser{Reader: bytes.NewReader([]byte("data")), closed: &r.closed}, Size: 4, Version: r.version}, nil
}

type trackingCloser struct {
	io.Reader
	closed *bool
}

func (r *trackingCloser) Close() error { *r.closed = true; return nil }

func TestResourceReaderRouterUsesExactAuthoritySchemeAndVersion(t *testing.T) {
	reader := &staticResourceReader{version: "v1"}
	router, err := NewResourceReaderRouter([]workcontract.ResourceReaderRoute{{Authority: "executor", Scheme: "session-file", Reader: reader}})
	if err != nil {
		t.Fatal(err)
	}
	backend := execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindRemote, Authority: "executor"}
	source := workmodel.ResourceRef{Authority: "executor", Scheme: "session-file", ID: "opaque", Version: "v1"}
	handle, err := router.Open(context.Background(), backend, source)
	if err != nil {
		t.Fatal(err)
	}
	_ = handle.Reader.Close()
	if _, err := router.Open(context.Background(), execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Authority: "host"}, source); !workspaceErrorIs(err, workcontract.ErrCodeProducedResourceBackendMismatch) {
		t.Fatalf("authority mismatch error = %v", err)
	}
	reader.version = "v2"
	reader.closed = false
	if _, err := router.Open(context.Background(), backend, source); !workspaceErrorIs(err, workcontract.ErrCodeProducedResourceVersionConflict) || !reader.closed {
		t.Fatalf("version mismatch error/close = %v/%v", err, reader.closed)
	}
}

func producedTestManifest() workmodel.RunManifest {
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", RunID: "run", AgentAppID: "app", AgentAppVersion: "1"}}
	profile := agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	return workmodel.RunManifest{SchemaVersion: workmodel.RunManifestSchemaVersion, Revision: 1, RunID: "run", Scope: workmodel.ResourceScope{TenantID: "tenant"}, AgentApp: profile, StateRoot: workmodel.StateRoot{ID: "state", Authority: "executor", Scope: workmodel.ResourceScope{TenantID: "tenant"}}, Limits: workmodel.WorkspaceLimits{MaximumAccess: execmodel.WorkspaceAccessReadWrite}, Executions: []workmodel.PreparedExecutionSnapshot{{Binding: binding, Backend: execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindRemote, Provider: "sandbox", Authority: "executor"}, Workspace: execmodel.ExecutionWorkspace{WorkDir: "/workspace/work", InputDir: "/workspace/input", OutputDir: "/workspace/output", TmpDir: "/workspace/tmp"}}}, CreatedAt: time.Now().UTC()}
}

