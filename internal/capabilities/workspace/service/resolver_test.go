package service

import (
	"context"
	"testing"

	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

func TestWorkspaceResolverUsesIntentNotAgentAppType(t *testing.T) {
	resolver, _ := NewWorkspaceResolver(&fixedIDs{values: []string{"1", "2"}})
	app := agentappmodel.EffectiveProfile{ID: "doc-review", Version: "v1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject, execmodel.WorkspaceModeTask, execmodel.WorkspaceModeSession}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	binding, err := resolver.Resolve(context.Background(), ResolveBindingRequest{Owner: execmodel.ExecutionOwnerRef{RunID: "run-1"}, Intent: workcontract.ExecutionIntent{ModifyProject: true, HasProject: true}, App: app, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Mode != execmodel.WorkspaceModeProject || binding.Owner.AgentAppID != "doc-review" {
		t.Fatalf("binding = %+v", binding)
	}
	job, err := resolver.Resolve(context.Background(), ResolveBindingRequest{Owner: execmodel.ExecutionOwnerRef{RunID: "run-2"}, Intent: workcontract.ExecutionIntent{BoundedInputs: true, BoundedOutputs: true}, App: app, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil || job.Mode != execmodel.WorkspaceModeTask {
		t.Fatalf("job = %+v, err = %v", job, err)
	}
}

func TestWorkspaceResolverRejectsExplicitModeOutsideIntersection(t *testing.T) {
	resolver, _ := NewWorkspaceResolver(&fixedIDs{values: []string{"1"}})
	_, err := resolver.Resolve(context.Background(), ResolveBindingRequest{Owner: execmodel.ExecutionOwnerRef{RunID: "run-1"}, Intent: workcontract.ExecutionIntent{ExplicitMode: execmodel.WorkspaceModeProject, HasProject: true}, App: agentappmodel.EffectiveProfile{ID: "app", Version: "v1", Workspace: agentappmodel.WorkspaceSpec{AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}}}, MaximumAccess: execmodel.WorkspaceAccessReadWrite})
	if err == nil {
		t.Fatal("Resolve() error = nil")
	}
}

func TestWorkspaceResolverAppSwitchGetsNewBinding(t *testing.T) {
	resolver, _ := NewWorkspaceResolver(&fixedIDs{values: []string{"1", "2"}})
	request := ResolveBindingRequest{Owner: execmodel.ExecutionOwnerRef{RunID: "run-1"}, Intent: workcontract.ExecutionIntent{BoundedInputs: true, BoundedOutputs: true}, MaximumAccess: execmodel.WorkspaceAccessReadWrite}
	request.App = agentappmodel.EffectiveProfile{ID: "app-a", Version: "v1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask}}
	first, _ := resolver.Resolve(context.Background(), request)
	request.App = agentappmodel.EffectiveProfile{ID: "app-b", Version: "v1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask}}
	second, _ := resolver.Resolve(context.Background(), request)
	if first.ID == second.ID || first.Owner.AgentAppID == second.Owner.AgentAppID {
		t.Fatalf("bindings were reused across apps: %+v %+v", first, second)
	}
}

func TestWorkspaceResolverRequiresExplicitAccessCeiling(t *testing.T) {
	resolver, _ := NewWorkspaceResolver(&fixedIDs{values: []string{"1"}})
	app := agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask}}
	_, err := resolver.Resolve(context.Background(), ResolveBindingRequest{Owner: execmodel.ExecutionOwnerRef{RunID: "run"}, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask}, App: app})
	if err == nil {
		t.Fatal("missing access ceiling must fail closed")
	}
}
