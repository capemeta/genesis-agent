package service

import (
	"context"
	"errors"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type failingViewProjector struct{}

func (failingViewProjector) Project(context.Context, workmodel.PreparedExecutionSnapshot, workmodel.InputManifest) (workmodel.WorkspaceViewManifest, error) {
	return workmodel.WorkspaceViewManifest{}, errors.New("projection failed")
}

func TestWorkspaceViewBuilderRollsBackSnapshotsWhenProjectionFails(t *testing.T) {
	store := &memoryStore{values: map[workmodel.WorkspacePath][]byte{}}
	stager, err := NewInputStager(memoryReader{"source": []byte("payload")}, store, &fixedIDs{values: []string{"input-1"}})
	if err != nil {
		t.Fatal(err)
	}
	builder, err := NewWorkspaceViewBuilder(stager, failingViewProjector{}, store)
	if err != nil {
		t.Fatal(err)
	}
	binding := execmodel.ExecutionBinding{ID: "binding-1", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1"}}
	_, _, err = builder.Bind(context.Background(), workmodel.PreparedExecutionSnapshot{Binding: binding}, []workmodel.ResourceRef{{Authority: "host", Scheme: "file", ID: "source", Path: "source.txt"}})
	if err == nil {
		t.Fatal("Bind() error = nil")
	}
	if len(store.values) != 0 {
		t.Fatalf("projection failure leaked snapshots: %+v", store.values)
	}
}

var _ workcontract.WorkspaceViewProjector = failingViewProjector{}
