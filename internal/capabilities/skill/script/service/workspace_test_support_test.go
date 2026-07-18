package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type testProvisioner struct{}

func (testProvisioner) Prepare(_ context.Context, req workcontract.PrepareRequest) (workcontract.PreparedExecution, error) {
	base := filepath.Join(req.StateRoot.Path, "runtime", "runs", req.Binding.Owner.RunID)
	w := execmodel.ExecutionWorkspace{WorkDir: filepath.Join(base, "work", req.Binding.ID), InputDir: filepath.Join(base, "input", req.Binding.ID), OutputDir: filepath.Join(base, "output", req.Binding.ID), TmpDir: filepath.Join(base, "tmp", req.Binding.ID)}
	for _, dir := range []string{w.WorkDir, w.InputDir, w.OutputDir, w.TmpDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return workcontract.PreparedExecution{}, err
		}
	}
	return workcontract.PreparedExecution{Binding: req.Binding, Workspace: w}, nil
}

type testSnapshotReader map[workmodel.WorkspacePath][]byte

func (r testSnapshotReader) OpenSnapshot(_ context.Context, path workmodel.WorkspacePath) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r[path])), nil
}

func testInputManifest(binding execmodel.ExecutionBinding, name string, content []byte) (workmodel.InputManifest, testSnapshotReader) {
	path := workmodel.WorkspacePath("runs/" + binding.Owner.RunID + "/input/input-test/" + name)
	digest := sha256.Sum256(content)
	manifest := workmodel.InputManifest{RunID: binding.Owner.RunID, BindingID: binding.ID, Inputs: []workmodel.InputRef{{ID: "input-test", Name: name, Alias: workmodel.WorkspacePath(name), Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:]), StagedPath: path}}}
	return manifest, testSnapshotReader{path: append([]byte(nil), content...)}
}

func testBinding(runID string) execmodel.ExecutionBinding {
	return execmodel.ExecutionBinding{ID: runID + "-skill-demo", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyStrictWorkspace, Owner: execmodel.ExecutionOwnerRef{RunID: runID}}
}

func testStateRoot(root string) workmodel.StateRoot {
	return workmodel.StateRoot{ID: "test-state", Authority: "host", Path: filepath.Join(root, ".genesis")}
}
