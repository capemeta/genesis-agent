package service

import (
	"context"
	"fmt"
	"testing"

	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmemory "genesis-agent/internal/capabilities/workspace/adapter/memory"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type fixedStateRoot struct{}

func (fixedStateRoot) ResolveStateRoot(_ context.Context, req workcontract.StateRootRequest) (workmodel.StateRoot, error) {
	return workmodel.StateRoot{ID: "state-1", Authority: "test", Scope: req.Scope}, nil
}

type fixedProvisioner struct{}

func (fixedProvisioner) Prepare(_ context.Context, req workcontract.PrepareRequest) (workcontract.PreparedExecution, error) {
	root := "/workspace/" + req.Binding.ID
	return workcontract.PreparedExecution{Binding: req.Binding, Backend: execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Authority: "test"}, Workspace: execmodel.ExecutionWorkspace{WorkDir: root + "/work", InputDir: root + "/input", OutputDir: root + "/output", TmpDir: root + "/tmp"}}, nil
}

type bindingMutatingProvisioner struct{}

func (bindingMutatingProvisioner) Prepare(_ context.Context, req workcontract.PrepareRequest) (workcontract.PreparedExecution, error) {
	prepared, _ := (fixedProvisioner{}).Prepare(context.Background(), req)
	prepared.Binding.Access = execmodel.WorkspaceAccessReadOnly
	return prepared, nil
}

type captureManifestStore struct{ value workmodel.RunManifest }

func (s *captureManifestStore) Create(_ context.Context, value workmodel.RunManifest) error {
	s.value = value
	return nil
}

type conflictOnceManifestStore struct {
	delegate *workmemory.ManifestStore
	conflict bool
}

func (s *conflictOnceManifestStore) Create(ctx context.Context, manifest workmodel.RunManifest) error {
	return s.delegate.Create(ctx, manifest)
}

func (s *conflictOnceManifestStore) Get(ctx context.Context, tenantID, runID string) (workmodel.RunManifest, error) {
	return s.delegate.Get(ctx, tenantID, runID)
}

func (s *conflictOnceManifestStore) AddExecution(ctx context.Context, tenantID, runID string, expectedRevision uint64, execution workmodel.PreparedExecutionSnapshot) (workmodel.RunManifest, error) {
	if s.conflict {
		s.conflict = false
		competing := execution
		competing.Binding.ID = "binding-competing"
		competing.Binding.Owner.TaskID = "hook:competing"
		if _, err := s.delegate.AddExecution(ctx, tenantID, runID, expectedRevision, competing); err != nil {
			return workmodel.RunManifest{}, err
		}
		return workmodel.RunManifest{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("simulated CAS conflict"))
	}
	return s.delegate.AddExecution(ctx, tenantID, runID, expectedRevision, execution)
}
func (s *captureManifestStore) Get(_ context.Context, _, _ string) (workmodel.RunManifest, error) {
	return s.value, nil
}
func (s *captureManifestStore) AddExecution(_ context.Context, _, _ string, _ uint64, execution workmodel.PreparedExecutionSnapshot) (workmodel.RunManifest, error) {
	s.value.Executions = append(s.value.Executions, execution)
	s.value.Revision++
	return s.value, nil
}

func TestRunPreparerFreezesIdentityBindingAndManifestBeforeEngine(t *testing.T) {
	ids := &fixedIDs{values: []string{"run-id", "binding-id"}}
	resolver, err := NewWorkspaceResolver(ids)
	if err != nil {
		t.Fatal(err)
	}
	store := &captureManifestStore{}
	preparer, err := NewRunPreparer(ids, resolver, fixedStateRoot{}, fixedProvisioner{}, store)
	if err != nil {
		t.Fatal(err)
	}
	profile := agentappmodel.EffectiveProfile{ID: "doc-review", Version: "v2", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	prepared, err := preparer.PrepareRun(context.Background(), workcontract.PrepareRunRequest{Scope: workmodel.ResourceScope{TenantID: "tenant-a"}, SessionID: "session-a", ParentRunID: "parent-a", App: profile, Intent: workcontract.ExecutionIntent{BoundedInputs: true, BoundedOutputs: true}, ProductModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}, BackendModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Manifest.RunID != "run-run-id" || prepared.Execution.Binding.ID != "binding-binding-id" {
		t.Fatalf("prepared identity = %+v", prepared)
	}
	if prepared.Manifest.SchemaVersion != "2" || prepared.Execution.Backend.Authority != "test" {
		t.Fatalf("manifest/backend snapshot = %+v", prepared)
	}
	if prepared.Execution.Binding.Owner.ParentRunID != "parent-a" || prepared.Execution.Binding.Owner.AgentAppID != "doc-review" {
		t.Fatalf("owner = %+v", prepared.Execution.Binding.Owner)
	}
	if err := store.value.Validate(); err != nil {
		t.Fatalf("stored manifest invalid: %v", err)
	}
}

func TestRunPreparerRejectsProvisionerBindingMutation(t *testing.T) {
	ids := &fixedIDs{values: []string{"run-id", "binding-id"}}
	resolver, _ := NewWorkspaceResolver(ids)
	preparer, _ := NewRunPreparer(ids, resolver, fixedStateRoot{}, bindingMutatingProvisioner{}, &captureManifestStore{})
	profile := agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	_, err := preparer.PrepareRun(context.Background(), workcontract.PrepareRunRequest{App: profile, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask}, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err == nil {
		t.Fatal("provisioner must not mutate immutable binding")
	}
}

func TestRunPreparerReusesStableDerivedExecutionAndAppendsManifest(t *testing.T) {
	ids := &fixedIDs{values: []string{"run-id", "root-binding", "skill-binding"}}
	resolver, _ := NewWorkspaceResolver(ids)
	store := workmemory.NewManifestStore()
	preparer, _ := NewRunPreparer(ids, resolver, fixedStateRoot{}, fixedProvisioner{}, store)
	profile := agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask, execmodel.WorkspaceModeSession}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	root, err := preparer.PrepareRun(context.Background(), workcontract.PrepareRunRequest{App: profile, Intent: workcontract.ExecutionIntent{BoundedInputs: true, BoundedOutputs: true}, ProductModes: profile.Workspace.AllowedModes, BackendModes: profile.Workspace.AllowedModes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	ctx := workcontract.WithPreparedRun(context.Background(), root)
	req := workcontract.PrepareExecutionRequest{Subject: execmodel.ExecutionSubjectRef{TaskID: "skill:demo"}, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeSession}, RequestedAccess: execmodel.WorkspaceAccessReadWrite}
	first, err := preparer.PrepareExecution(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := preparer.PrepareExecution(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Binding.ID != second.Binding.ID {
		t.Fatalf("稳定派生主体不得重复创建 binding: %s != %s", first.Binding.ID, second.Binding.ID)
	}
	manifest, _ := store.Get(context.Background(), root.Manifest.Scope.TenantID, root.Manifest.RunID)
	if manifest.Revision != 2 || len(manifest.Executions) != 2 {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestRunPreparerRetriesCASForDifferentConcurrentSubject(t *testing.T) {
	ids := &fixedIDs{values: []string{"run-id", "root-binding", "skill-binding"}}
	resolver, _ := NewWorkspaceResolver(ids)
	store := &conflictOnceManifestStore{delegate: workmemory.NewManifestStore(), conflict: true}
	preparer, _ := NewRunPreparer(ids, resolver, fixedStateRoot{}, fixedProvisioner{}, store)
	profile := agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask, execmodel.WorkspaceModeSession}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	root, err := preparer.PrepareRun(context.Background(), workcontract.PrepareRunRequest{Scope: workmodel.ResourceScope{TenantID: "tenant"}, App: profile, Intent: workcontract.ExecutionIntent{BoundedInputs: true, BoundedOutputs: true}, ProductModes: profile.Workspace.AllowedModes, BackendModes: profile.Workspace.AllowedModes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	ctx := workcontract.WithPreparedRun(context.Background(), root)
	result, err := preparer.PrepareExecution(ctx, workcontract.PrepareExecutionRequest{Subject: execmodel.ExecutionSubjectRef{TaskID: "skill:demo"}, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeSession}, RequestedAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	if result.Binding.Owner.TaskID != "skill:demo" {
		t.Fatalf("unexpected execution: %+v", result)
	}
	manifest, err := store.Get(context.Background(), "tenant", root.Manifest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Revision != 3 || len(manifest.Executions) != 3 {
		t.Fatalf("CAS retry did not preserve both executions: %+v", manifest)
	}
}

func TestRunPreparerRejectsReuseThatWouldBroadenCurrentRequest(t *testing.T) {
	ids := &fixedIDs{values: []string{"run-id", "root-binding", "skill-binding"}}
	resolver, _ := NewWorkspaceResolver(ids)
	store := workmemory.NewManifestStore()
	preparer, _ := NewRunPreparer(ids, resolver, fixedStateRoot{}, fixedProvisioner{}, store)
	profile := agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask, execmodel.WorkspaceModeSession}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	root, err := preparer.PrepareRun(context.Background(), workcontract.PrepareRunRequest{App: profile, Intent: workcontract.ExecutionIntent{BoundedInputs: true, BoundedOutputs: true}, ProductModes: profile.Workspace.AllowedModes, BackendModes: profile.Workspace.AllowedModes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	ctx := workcontract.WithPreparedRun(context.Background(), root)
	subject := execmodel.ExecutionSubjectRef{TaskID: "skill:demo"}
	if _, err := preparer.PrepareExecution(ctx, workcontract.PrepareExecutionRequest{Subject: subject, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeSession}, RequestedAccess: execmodel.WorkspaceAccessReadWrite}); err != nil {
		t.Fatal(err)
	}
	_, err = preparer.PrepareExecution(ctx, workcontract.PrepareExecutionRequest{Subject: subject, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeSession}, RequestedAccess: execmodel.WorkspaceAccessReadOnly})
	if err == nil {
		t.Fatal("只读调用不得复用既有可写 binding")
	}
}

func TestRunPreparerUsesAuthoritativeBaseExecution(t *testing.T) {
	ids := &fixedIDs{values: []string{"run-id", "root-binding", "step-binding"}}
	resolver, _ := NewWorkspaceResolver(ids)
	store := workmemory.NewManifestStore()
	preparer, _ := NewRunPreparer(ids, resolver, fixedStateRoot{}, fixedProvisioner{}, store)
	profile := agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	root, err := preparer.PrepareRun(context.Background(), workcontract.PrepareRunRequest{Scope: workmodel.ResourceScope{TenantID: "tenant"}, SessionID: "trusted-session", App: profile, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask}, ProductModes: profile.Workspace.AllowedModes, BackendModes: profile.Workspace.AllowedModes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	forged := root
	forged.Execution.Binding.Owner.SessionID = "forged-session"
	ctx := workcontract.WithPreparedRun(context.Background(), forged)
	derived, err := preparer.PrepareExecution(ctx, workcontract.PrepareExecutionRequest{Subject: execmodel.ExecutionSubjectRef{WorkflowStepID: "step"}, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask}, RequestedAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	if derived.Binding.Owner.SessionID != "trusted-session" {
		t.Fatalf("derived owner trusted caller snapshot: %+v", derived.Binding.Owner)
	}

	unknown := root
	unknown.Execution.Binding.ID = "unknown-binding"
	if _, err := preparer.PrepareExecution(workcontract.WithPreparedRun(context.Background(), unknown), workcontract.PrepareExecutionRequest{Subject: execmodel.ExecutionSubjectRef{WorkflowStepID: "other-step"}, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask}}); err == nil {
		t.Fatal("unknown context binding must be rejected")
	}
}

func TestRunPreparerDerivedExecutionInheritsProjectAuthorization(t *testing.T) {
	ids := &fixedIDs{values: []string{"run-id", "root-binding", "skill-binding"}}
	resolver, _ := NewWorkspaceResolver(ids)
	store := workmemory.NewManifestStore()
	preparer, _ := NewRunPreparer(ids, resolver, fixedStateRoot{}, fixedProvisioner{}, store)
	modes := []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject, execmodel.WorkspaceModeTask}
	profile := agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeProject, AllowedModes: modes, RequiresProject: true, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	scope := workmodel.ResourceScope{ProjectID: "project"}
	project := &workmodel.ResourceRef{Authority: "host", Scheme: "project", ID: "project", Scope: scope}
	root, err := preparer.PrepareRun(context.Background(), workcontract.PrepareRunRequest{Scope: scope, App: profile, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeProject, HasProject: true}, ProjectRoot: project, ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	derived, err := preparer.PrepareExecution(workcontract.WithPreparedRun(context.Background(), root), workcontract.PrepareExecutionRequest{Subject: execmodel.ExecutionSubjectRef{TaskID: "skill:demo"}, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask}, RequestedAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatalf("派生 execution 应继承 Run 的项目授权事实: %v", err)
	}
	if derived.Binding.Mode != execmodel.WorkspaceModeTask {
		t.Fatalf("mode = %s, want task_job", derived.Binding.Mode)
	}
}
