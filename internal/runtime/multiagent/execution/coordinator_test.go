package execution

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	agentappmemory "genesis-agent/internal/capabilities/agentapp/adapter/memory"
	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmemory "genesis-agent/internal/capabilities/workspace/adapter/memory"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	workservice "genesis-agent/internal/capabilities/workspace/service"
)

type sequenceIDs struct{ next atomic.Uint64 }

func (s *sequenceIDs) Generate() string { return fmt.Sprintf("%d", s.next.Add(1)) }

type testStateRoot struct{}

func (testStateRoot) ResolveStateRoot(_ context.Context, req workcontract.StateRootRequest) (workmodel.StateRoot, error) {
	return workmodel.StateRoot{ID: "state:" + req.RunID, Authority: "test", Scope: req.Scope}, nil
}

type testProvisioner struct{}

func (testProvisioner) Prepare(_ context.Context, req workcontract.PrepareRequest) (workcontract.PreparedExecution, error) {
	base := "/workspace/" + req.Binding.ID
	workspace := execmodel.ExecutionWorkspace{WorkDir: base + "/work", InputDir: base + "/input", OutputDir: base + "/output", TmpDir: base + "/tmp"}
	backend := req.Backend
	if backend.Kind == "" {
		backend = execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Authority: "test"}
	}
	return workcontract.PreparedExecution{Binding: req.Binding, Backend: backend, Workspace: workspace}, nil
}

func TestCoordinatorIsolatesWorkflowStepsAndCollaborationMember(t *testing.T) {
	ctx := context.Background()
	ids := &sequenceIDs{}
	resolver, err := workservice.NewWorkspaceResolver(ids)
	if err != nil {
		t.Fatal(err)
	}
	control, err := workservice.NewRunPreparer(ids, resolver, testStateRoot{}, testProvisioner{}, workmemory.NewManifestStore())
	if err != nil {
		t.Fatal(err)
	}
	modes := []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}
	appA := testApp("app-a", modes)
	appB := testApp("app-b", modes)
	apps, err := agentappmemory.NewResolver(appA.ID, []agentappmodel.EffectiveProfile{appA, appB})
	if err != nil {
		t.Fatal(err)
	}
	root, err := control.PrepareRun(ctx, workcontract.PrepareRunRequest{Scope: workmodel.ResourceScope{TenantID: "tenant", ProjectID: "project", UserID: "user"}, SessionID: "session", App: appA, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask}, ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	ctx = workcontract.WithPreparedRun(ctx, root)
	coordinator, err := NewCoordinator(control, apps, RuntimePolicy{ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	stepA, err := coordinator.PrepareWorkflowStep(ctx, WorkflowStepRequest{StepID: "step-a"})
	if err != nil {
		t.Fatal(err)
	}
	stepB, err := coordinator.PrepareWorkflowStep(ctx, WorkflowStepRequest{StepID: "step-b"})
	if err != nil {
		t.Fatal(err)
	}
	if stepA.Binding.ID == stepB.Binding.ID || stepA.Workspace.WorkDir == stepB.Workspace.WorkDir {
		t.Fatalf("workflow steps share writable namespace: A=%+v B=%+v", stepA, stepB)
	}
	again, err := coordinator.PrepareWorkflowStep(ctx, WorkflowStepRequest{StepID: "step-a"})
	if err != nil || again.Binding.ID != stepA.Binding.ID {
		t.Fatalf("same workflow step must reuse stable binding: again=%+v err=%v", again, err)
	}
	member, err := coordinator.PrepareCollaborationMember(ctx, CollaborationMemberRequest{CollaborationSpaceID: "space-1", MemberID: "member-b", AppID: "app-b"})
	if err != nil {
		t.Fatal(err)
	}
	if member.Manifest.RunID == root.Manifest.RunID || member.Manifest.ParentRunID != root.Manifest.RunID {
		t.Fatalf("collaboration member must own child Run: %+v", member.Manifest)
	}
	owner := member.Execution.Binding.Owner
	if member.Manifest.AgentApp.ID != "app-b" || owner.AgentAppID != "app-b" || owner.CollaborationSpaceID != "space-1" || owner.MemberID != "member-b" {
		t.Fatalf("member identity/App not frozen: manifest=%+v owner=%+v", member.Manifest.AgentApp, owner)
	}
	if member.Execution.Workspace.WorkDir == root.Execution.Workspace.WorkDir {
		t.Fatal("collaboration member reused parent writable cwd")
	}
}

func TestCoordinatorRejectsForgedParentScope(t *testing.T) {
	ctx, coordinator, root := testCoordinator(t)
	forged := root
	forged.Manifest.Scope.TenantID = "other-tenant"
	ctx = workcontract.WithPreparedRun(ctx, forged)
	if _, err := coordinator.PrepareWorkflowStep(ctx, WorkflowStepRequest{StepID: "step"}); err == nil {
		t.Fatal("forged parent scope must fail")
	}
}

func TestCoordinatorRejectsImplicitMemberAppAndFabricatedProject(t *testing.T) {
	ctx := context.Background()
	ids := &sequenceIDs{}
	resolver, _ := workservice.NewWorkspaceResolver(ids)
	control, _ := workservice.NewRunPreparer(ids, resolver, testStateRoot{}, testProvisioner{}, workmemory.NewManifestStore())
	modes := []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject, execmodel.WorkspaceModeTask}
	app := testApp("app", modes)
	apps, _ := agentappmemory.NewResolver(app.ID, []agentappmodel.EffectiveProfile{app})
	root, err := control.PrepareRun(ctx, workcontract.PrepareRunRequest{Scope: workmodel.ResourceScope{TenantID: "tenant"}, App: app, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask}, ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	coordinator, _ := NewCoordinator(control, apps, RuntimePolicy{ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	ctx = workcontract.WithPreparedRun(ctx, root)
	if _, err := coordinator.PrepareCollaborationMember(ctx, CollaborationMemberRequest{CollaborationSpaceID: "space", MemberID: "member"}); err == nil {
		t.Fatal("collaboration member must select an explicit Agent App")
	}
	if _, err := coordinator.PrepareCollaborationMember(ctx, CollaborationMemberRequest{CollaborationSpaceID: "space", MemberID: "member", AppID: "app", Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeProject, HasProject: true}}); err == nil {
		t.Fatal("member request must not fabricate project authorization")
	}
}

func TestCoordinatorMemberInheritsOnlyParentProject(t *testing.T) {
	ctx := context.Background()
	ids := &sequenceIDs{}
	resolver, _ := workservice.NewWorkspaceResolver(ids)
	control, _ := workservice.NewRunPreparer(ids, resolver, testStateRoot{}, testProvisioner{}, workmemory.NewManifestStore())
	modes := []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject, execmodel.WorkspaceModeTask}
	app := testApp("app", modes)
	apps, _ := agentappmemory.NewResolver(app.ID, []agentappmodel.EffectiveProfile{app})
	scope := workmodel.ResourceScope{TenantID: "tenant", ProjectID: "project"}
	project := &workmodel.ResourceRef{Authority: "host", Scheme: "project", ID: "project", Version: "v1", Scope: scope}
	root, err := control.PrepareRun(ctx, workcontract.PrepareRunRequest{Scope: scope, App: app, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeProject, HasProject: true}, ProjectRoot: project, ProjectDir: "project:/", ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	coordinator, _ := NewCoordinator(control, apps, RuntimePolicy{ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	member, err := coordinator.PrepareCollaborationMember(workcontract.WithPreparedRun(ctx, root), CollaborationMemberRequest{CollaborationSpaceID: "space", MemberID: "member", AppID: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if member.Manifest.ProjectRoot == nil || *member.Manifest.ProjectRoot != *project || member.Manifest.ProjectDir != root.Manifest.ProjectDir {
		t.Fatalf("member project authority diverged from parent: parent=%+v member=%+v", root.Manifest, member.Manifest)
	}
}

func testCoordinator(t *testing.T) (context.Context, *Coordinator, workmodel.PreparedRun) {
	t.Helper()
	ctx := context.Background()
	ids := &sequenceIDs{}
	resolver, _ := workservice.NewWorkspaceResolver(ids)
	control, _ := workservice.NewRunPreparer(ids, resolver, testStateRoot{}, testProvisioner{}, workmemory.NewManifestStore())
	modes := []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}
	app := testApp("app", modes)
	apps, _ := agentappmemory.NewResolver(app.ID, []agentappmodel.EffectiveProfile{app})
	root, err := control.PrepareRun(ctx, workcontract.PrepareRunRequest{Scope: workmodel.ResourceScope{TenantID: "tenant"}, App: app, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask}, ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	coordinator, _ := NewCoordinator(control, apps, RuntimePolicy{ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	return ctx, coordinator, root
}

func testApp(id string, modes []execmodel.WorkspaceMode) agentappmodel.EffectiveProfile {
	return agentappmodel.EffectiveProfile{ID: id, Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: append([]execmodel.WorkspaceMode(nil), modes...), DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
}
