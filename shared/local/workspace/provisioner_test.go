package workspace

import (
	"context"
	"path/filepath"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestProvisionerCreatesIsolatedBindingDirectories(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	binding := execmodel.ExecutionBinding{ID: "binding-1", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyStrictWorkspace, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1"}}
	prepared, err := NewProvisioner().Prepare(context.Background(), workcontract.PrepareRequest{StateRoot: workmodel.StateRoot{ID: "state-1", Authority: "host", Path: stateRoot}, Binding: binding})
	if err != nil {
		t.Fatal(err)
	}
	w := prepared.Workspace
	if w.WorkDir == w.InputDir || w.WorkDir == w.OutputDir || w.InputDir == w.OutputDir {
		t.Fatalf("task directories are not isolated: %+v", w)
	}
	wantWork := filepath.Join(stateRoot, "runtime", "runs", "run-1", "work", "binding-1")
	if w.WorkDir != wantWork {
		t.Fatalf("WorkDir = %q, want %q", w.WorkDir, wantWork)
	}
}

func TestProvisionerProjectRequiresExplicitProjectRoot(t *testing.T) {
	binding := execmodel.ExecutionBinding{ID: "binding-1", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1"}}
	_, err := NewProvisioner().Prepare(context.Background(), workcontract.PrepareRequest{StateRoot: workmodel.StateRoot{ID: "state-1", Authority: "host", Path: t.TempDir()}, Binding: binding})
	if err == nil {
		t.Fatal("Prepare() error = nil")
	}
}
