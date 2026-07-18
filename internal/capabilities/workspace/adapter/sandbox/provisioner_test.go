package sandbox

import (
	"context"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestSessionWorkspaceKeepsStableWorkDirAndIsolatesExecutionIO(t *testing.T) {
	provisioner := NewProvisioner()
	prepare := func(bindingID, runID string) workcontract.PreparedExecution {
		binding := execmodel.ExecutionBinding{ID: bindingID, Mode: execmodel.WorkspaceModeSession, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: runID, SessionID: "agent-session", TaskID: "sandbox:command"}}
		result, err := provisioner.Prepare(context.Background(), workcontract.PrepareRequest{
			StateRoot: workmodel.StateRoot{ID: "remote", Authority: "executor"}, Binding: binding,
			Backend: execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindRemote, Provider: "genesis-sandbox", Authority: "remote-executor"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := prepare("binding-one", "run-one")
	second := prepare("binding-two", "run-two")
	if first.Workspace.WorkDir != "/workspace/work" || second.Workspace.WorkDir != first.Workspace.WorkDir {
		t.Fatalf("session work dirs must be stable: first=%+v second=%+v", first.Workspace, second.Workspace)
	}
	if first.Workspace.InputDir == second.Workspace.InputDir || first.Workspace.OutputDir == second.Workspace.OutputDir || first.Workspace.TmpDir == second.Workspace.TmpDir {
		t.Fatalf("execution IO dirs must stay isolated: first=%+v second=%+v", first.Workspace, second.Workspace)
	}
}
