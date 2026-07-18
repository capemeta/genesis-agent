package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestViewProjectorCreatesNestedInputAndWorkingCopy(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := NewInputSnapshotStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("source")
	staged, err := store.Put(context.Background(), "run", "input-one", "source.pptx", bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	scope := workmodel.ResourceScope{TenantID: "tenant"}
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", RunID: "run"}}
	base := filepath.Join(stateRoot, "view")
	execution := workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: filepath.Join(base, "work"), InputDir: filepath.Join(base, "input"), OutputDir: filepath.Join(base, "output"), TmpDir: filepath.Join(base, "tmp")}}
	for _, dir := range []string{execution.Workspace.WorkDir, execution.Workspace.InputDir, execution.Workspace.OutputDir, execution.Workspace.TmpDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manifest := workmodel.InputManifest{RunID: "run", BindingID: "binding", Inputs: []workmodel.InputRef{{ID: "input-one", Name: "source.pptx", Alias: "docs/source.pptx", Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:]), Source: workmodel.ResourceRef{Authority: "host", Scheme: "file", ID: "source", Path: "docs/source.pptx", Scope: scope}, StagedPath: staged}}}
	projector, _ := NewViewProjector(store)
	view, err := projector.Project(context.Background(), execution, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 1 || view.Entries[0].Path != "docs/source.pptx" {
		t.Fatalf("view=%+v", view)
	}
	workPath := filepath.Join(execution.Workspace.WorkDir, "docs", "source.pptx")
	inputPath := filepath.Join(execution.Workspace.InputDir, "docs", "source.pptx")
	for _, target := range []string{workPath, inputPath} {
		got, readErr := os.ReadFile(target)
		if readErr != nil || !bytes.Equal(got, content) {
			t.Fatalf("projection %s=%q err=%v", target, got, readErr)
		}
	}
	if err := os.WriteFile(workPath, []byte("edited"), 0o600); err != nil {
		t.Fatalf("工作副本必须可编辑: %v", err)
	}
	canonical, _ := os.ReadFile(filepath.Join(stateRoot, filepath.FromSlash(string(staged))))
	if !bytes.Equal(canonical, content) {
		t.Fatalf("编辑工作副本污染不可变快照: %q", canonical)
	}
}
