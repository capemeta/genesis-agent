package handoff

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	agentappmemory "genesis-agent/internal/capabilities/agentapp/adapter/memory"
	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmemory "genesis-agent/internal/capabilities/workspace/adapter/memory"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	workservice "genesis-agent/internal/capabilities/workspace/service"
	multiexecution "genesis-agent/internal/runtime/multiagent/execution"
)

type handoffIDs struct{ next atomic.Uint64 }

func (s *handoffIDs) Generate() string { return fmt.Sprintf("%d", s.next.Add(1)) }

type receiptStore struct {
	mu    sync.Mutex
	items map[string]Receipt
}

func newReceiptStore() *receiptStore { return &receiptStore{items: make(map[string]Receipt)} }

func (s *receiptStore) PutIfAbsent(_ context.Context, receipt Receipt) (Receipt, bool, error) {
	key := receipt.TenantID + "\x00" + receipt.IdempotencyKey
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.items[key]; ok {
		return existing, false, nil
	}
	s.items[key] = receipt
	return receipt, true, nil
}

type handoffStateRoot struct{}

func (handoffStateRoot) ResolveStateRoot(_ context.Context, req workcontract.StateRootRequest) (workmodel.StateRoot, error) {
	return workmodel.StateRoot{ID: "state:" + req.RunID, Authority: "test", Scope: req.Scope}, nil
}

type handoffProvisioner struct{}

func (handoffProvisioner) Prepare(_ context.Context, req workcontract.PrepareRequest) (workcontract.PreparedExecution, error) {
	base := "/workspace/" + req.Binding.ID
	backend := req.Backend
	if backend.Kind == "" {
		backend = execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Authority: "test"}
	}
	return workcontract.PreparedExecution{Binding: req.Binding, Backend: backend, Workspace: execmodel.ExecutionWorkspace{WorkDir: base + "/work", InputDir: base + "/input", OutputDir: base + "/output", TmpDir: base + "/tmp"}}, nil
}

type recordingAuthorizer struct {
	denied bool
	calls  int
}

func (a *recordingAuthorizer) AuthorizeTransfer(context.Context, AuthorizationRequest) error {
	a.calls++
	if a.denied {
		return fmt.Errorf("denied")
	}
	return nil
}

func TestServiceTransfersStableReferencesAndIsIdempotent(t *testing.T) {
	ctx, control, source, target := handoffExecutions(t)
	auth := &recordingAuthorizer{}
	service, err := New(control, auth, newReceiptStore(), &handoffIDs{})
	if err != nil {
		t.Fatal(err)
	}
	scope := ownerScope(source.Binding.Owner)
	resource := workmodel.ResourceRef{Authority: "workspace", Scheme: "document", ID: "doc-1", Path: "documents/report.pdf", Version: "sha256:" + strings.Repeat("1", 64), Scope: scope}
	artifact := artifactmodel.ArtifactRef{ID: "artifact-1", Name: "summary.md", Size: 10, SHA256: strings.Repeat("2", 64), Producer: "test", RunID: source.Binding.Owner.RunID, Scope: scope, StorageRef: workmodel.ResourceRef{Authority: "object", Scheme: "artifact", ID: "object-1", Path: "artifacts/artifact-1/summary.md", Version: "sha256:" + strings.Repeat("2", 64), Scope: scope}}
	req := Request{TenantID: "tenant", IdempotencyKey: "handoff-key", Source: locator(source), Target: locator(target), Resources: []workmodel.ResourceRef{resource}, Artifacts: []artifactmodel.ArtifactRef{artifact}}
	first, err := service.Transfer(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Transfer(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Fingerprint != second.Fingerprint || auth.calls != 4 {
		t.Fatalf("idempotent transfer mismatch: first=%+v second=%+v calls=%d", first, second, auth.calls)
	}
	if filepath.IsAbs(first.Resources[0].Path) || filepath.IsAbs(first.Artifacts[0].StorageRef.Path) {
		t.Fatal("receipt leaked physical absolute path")
	}

	conflicting := req
	conflicting.Resources = []workmodel.ResourceRef{{Authority: "workspace", Scheme: "document", ID: "doc-2", Version: "sha256:" + strings.Repeat("3", 64), Scope: scope}}
	if _, err := service.Transfer(ctx, conflicting); err == nil {
		t.Fatal("same idempotency key with different payload must fail")
	}
}

func TestServiceRejectsCrossScopeAbsolutePathUnknownBindingAndDeniedTarget(t *testing.T) {
	ctx, control, source, target := handoffExecutions(t)
	scope := ownerScope(source.Binding.Owner)
	base := Request{TenantID: "tenant", IdempotencyKey: "key", Source: locator(source), Target: locator(target), Resources: []workmodel.ResourceRef{{Authority: "workspace", Scheme: "document", ID: "doc", Version: "v1", Scope: scope}}}

	tests := []struct {
		name string
		edit func(*Request)
	}{
		{name: "cross tenant scope", edit: func(req *Request) { req.Resources[0].Scope.TenantID = "other" }},
		{name: "absolute path", edit: func(req *Request) { req.Resources[0].Path = `C:\secret.txt` }},
		{name: "unknown binding", edit: func(req *Request) { req.Target.BindingID = "missing" }},
		{name: "unversioned resource", edit: func(req *Request) { req.Resources[0].Version = "" }},
		{name: "non canonical id", edit: func(req *Request) { req.Resources[0].ID = " doc " }},
		{name: "non canonical path", edit: func(req *Request) { req.Resources[0].Path = `documents\report.pdf` }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			req.Resources = append([]workmodel.ResourceRef(nil), base.Resources...)
			tt.edit(&req)
			service, _ := New(control, &recordingAuthorizer{}, newReceiptStore(), &handoffIDs{})
			if _, err := service.Transfer(ctx, req); err == nil {
				t.Fatal("invalid handoff must fail")
			}
		})
	}
	service, _ := New(control, &recordingAuthorizer{denied: true}, newReceiptStore(), &handoffIDs{})
	if _, err := service.Transfer(ctx, base); err == nil {
		t.Fatal("target authorization denial must fail")
	}
}

func TestServiceRejectsInvalidArtifactEvidence(t *testing.T) {
	ctx, control, source, target := handoffExecutions(t)
	scope := ownerScope(source.Binding.Owner)
	valid := artifactmodel.ArtifactRef{
		ID: "artifact", Name: "report.md", Size: 1, SHA256: strings.Repeat("a", 64), Producer: "test",
		RunID: source.Binding.Owner.RunID, Scope: scope,
		StorageRef: workmodel.ResourceRef{Authority: "object", Scheme: "artifact", ID: "object", Version: "sha256:" + strings.Repeat("a", 64), Scope: scope},
	}
	tests := []struct {
		name string
		edit func(*artifactmodel.ArtifactRef)
	}{
		{name: "invalid hash", edit: func(ref *artifactmodel.ArtifactRef) { ref.SHA256 = "not-a-sha256" }},
		{name: "uppercase hash", edit: func(ref *artifactmodel.ArtifactRef) { ref.SHA256 = strings.Repeat("A", 64) }},
		{name: "version mismatch", edit: func(ref *artifactmodel.ArtifactRef) { ref.StorageRef.Version = "sha256:" + strings.Repeat("b", 64) }},
		{name: "foreign run", edit: func(ref *artifactmodel.ArtifactRef) { ref.RunID = "other-run" }},
		{name: "unsafe name", edit: func(ref *artifactmodel.ArtifactRef) { ref.Name = "../report.md" }},
		{name: "missing scheme", edit: func(ref *artifactmodel.ArtifactRef) { ref.StorageRef.Scheme = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artifact := valid
			tt.edit(&artifact)
			service, _ := New(control, &recordingAuthorizer{}, newReceiptStore(), &handoffIDs{})
			_, err := service.Transfer(ctx, Request{TenantID: "tenant", IdempotencyKey: tt.name, Source: locator(source), Target: locator(target), Artifacts: []artifactmodel.ArtifactRef{artifact}})
			if err == nil {
				t.Fatal("invalid artifact evidence must fail")
			}
		})
	}
}

func handoffExecutions(t *testing.T) (context.Context, workcontract.ControlPlane, workmodel.PreparedExecutionSnapshot, workmodel.PreparedExecutionSnapshot) {
	t.Helper()
	ctx := context.Background()
	ids := &handoffIDs{}
	resolver, _ := workservice.NewWorkspaceResolver(ids)
	control, _ := workservice.NewRunPreparer(ids, resolver, handoffStateRoot{}, handoffProvisioner{}, workmemory.NewManifestStore())
	modes := []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}
	appA := handoffApp("app-a", modes)
	appB := handoffApp("app-b", modes)
	apps, _ := agentappmemory.NewResolver(appA.ID, []agentappmodel.EffectiveProfile{appA, appB})
	scope := workmodel.ResourceScope{TenantID: "tenant", ProjectID: "project", UserID: "user"}
	root, err := control.PrepareRun(ctx, workcontract.PrepareRunRequest{Scope: scope, SessionID: "session", App: appA, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask}, ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	ctx = workcontract.WithPreparedRun(ctx, root)
	coordinator, _ := multiexecution.NewCoordinator(control, apps, multiexecution.RuntimePolicy{ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	member, err := coordinator.PrepareCollaborationMember(ctx, multiexecution.CollaborationMemberRequest{CollaborationSpaceID: "space", MemberID: "member", AppID: "app-b"})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, control, root.Execution, member.Execution
}

func handoffApp(id string, modes []execmodel.WorkspaceMode) agentappmodel.EffectiveProfile {
	return agentappmodel.EffectiveProfile{ID: id, Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: modes, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
}

func locator(execution workmodel.PreparedExecutionSnapshot) BindingLocator {
	return BindingLocator{RunID: execution.Binding.Owner.RunID, BindingID: execution.Binding.ID}
}

func ownerScope(owner execmodel.ExecutionOwnerRef) workmodel.ResourceScope {
	return workmodel.ResourceScope{TenantID: owner.TenantID, ProjectID: owner.ProjectID, UserID: owner.UserID}
}
