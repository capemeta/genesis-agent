package service

import (
	"bytes"
	"context"
	"io"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type fixedIDs struct{ values []string }

func (f *fixedIDs) Generate() string { value := f.values[0]; f.values = f.values[1:]; return value }

type memoryReader map[string][]byte

func (r memoryReader) Open(_ context.Context, ref workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	data := r[ref.ID]
	return workcontract.ResourceHandle{Reader: io.NopCloser(bytes.NewReader(data)), Size: int64(len(data))}, nil
}

type memoryStore struct {
	values map[workmodel.WorkspacePath][]byte
}

func (s *memoryStore) Put(_ context.Context, runID, inputID, name string, content io.Reader) (workmodel.WorkspacePath, error) {
	data, _ := io.ReadAll(content)
	p := workmodel.WorkspacePath("runs/" + runID + "/input/" + inputID + "/" + name)
	s.values[p] = data
	return p, nil
}
func (s *memoryStore) Remove(_ context.Context, p workmodel.WorkspacePath) error {
	delete(s.values, p)
	return nil
}

func TestInputStagerRenamesDuplicatesAndHashes(t *testing.T) {
	store := &memoryStore{values: map[workmodel.WorkspacePath][]byte{}}
	stager, err := NewInputStager(memoryReader{"a": []byte("first"), "b": []byte("second")}, store, &fixedIDs{values: []string{"1", "2"}})
	if err != nil {
		t.Fatal(err)
	}
	binding := execmodel.ExecutionBinding{ID: "binding-1", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1"}}
	manifest, err := stager.Stage(context.Background(), workcontract.StageRequest{Binding: binding, Sources: []workmodel.ResourceRef{
		{Authority: "host", Scheme: "file", ID: "a", Path: "folder/report.pdf"},
		{Authority: "host", Scheme: "file", ID: "b", Path: "other/report.pdf"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Inputs) != 2 || manifest.Inputs[0].Name != "report.pdf" || manifest.Inputs[1].Name != "report-2.pdf" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if manifest.Inputs[0].SHA256 == "" || manifest.Inputs[0].StagedPath == manifest.Inputs[1].StagedPath {
		t.Fatalf("invalid immutable inputs: %+v", manifest.Inputs)
	}
}

func TestInputStagerRemovesOversizedSnapshot(t *testing.T) {
	store := &memoryStore{values: map[workmodel.WorkspacePath][]byte{}}
	stager, _ := NewInputStager(memoryReader{"a": []byte("too-large")}, store, &fixedIDs{values: []string{"1"}})
	binding := execmodel.ExecutionBinding{ID: "binding-1", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1"}}
	_, err := stager.Stage(context.Background(), workcontract.StageRequest{Binding: binding, Sources: []workmodel.ResourceRef{{Authority: "host", Scheme: "file", ID: "a", Path: "a.txt"}}, MaxFileSize: 3})
	if err == nil {
		t.Fatal("Stage() error = nil")
	}
	if len(store.values) != 0 {
		t.Fatalf("oversized snapshot leaked: %+v", store.values)
	}
}
