package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestResourceRegistryFreezesVersionAndRejectsMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "report.md")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewResourceRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := resourceRegistryContext(root)
	refs, err := registry.ResolveInputs(ctx, []string{"report.md"})
	if err != nil || len(refs) != 1 {
		t.Fatalf("resolve refs=%v err=%v", refs, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	_, err = registry.Open(context.Background(), refs[0])
	var classified *workcontract.Error
	if !errors.As(err, &classified) || classified.Code != workcontract.ErrCodeResourceVersionConflict {
		t.Fatalf("应返回版本冲突，实际: %v", err)
	}
}

func TestResourceRegistryIDsAreScopeIsolated(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.md"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewResourceRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	firstScope := workmodel.ResourceScope{TenantID: "tenant-a", ProjectID: "project", UserID: "user"}
	secondScope := workmodel.ResourceScope{TenantID: "tenant-b", ProjectID: "project", UserID: "user"}
	first, _, err := registry.register(context.Background(), filepath.Join(root, "report.md"), "report.md", firstScope)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := registry.register(context.Background(), filepath.Join(root, "report.md"), "report.md", secondScope)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.Version == "" || first.Version != second.Version {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	for _, ref := range []workmodel.ResourceRef{first, second} {
		handle, openErr := registry.Open(context.Background(), ref)
		if openErr != nil {
			t.Fatal(openErr)
		}
		_ = handle.Reader.Close()
	}
}

func TestResourceRegistryRejectsRelativeTraversal(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewResourceRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ResolveInputs(resourceRegistryContext(root), []string{"../secret.txt"}); err == nil {
		t.Fatal("相对路径不得越过 project root")
	}
}

func TestResourceRegistryOptionalInputsSkipOnlyMissingFiles(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	registry, err := NewResourceRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := registry.ResolveAvailableInputs(resourceRegistryContext(root), []string{"package_script.py"})
	if err != nil || len(refs) != 0 {
		t.Fatalf("missing optional entry refs=%v err=%v", refs, err)
	}
	if _, err := registry.ResolveAvailableInputs(resourceRegistryContext(root), []string{"../outside.py"}); err == nil {
		t.Fatal("optional entry must not weaken workspace boundary")
	}
}

func TestResourceRegistryPlansOnlyExistingExactPromptInputs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "source.pptx"), []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "output.pptx"), []byte("old output"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewResourceRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	scope := workmodel.ResourceScope{TenantID: "tenant", ProjectID: "project", UserID: "user"}
	refs, err := registry.PlanRequestInputs(context.Background(), workcontract.RequestInputRequest{Prompt: "请把 docs/source.pptx 修改后另存为 output.pptx", Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Path != "docs/source.pptx" || refs[0].Scope != scope {
		t.Fatalf("planned refs=%+v", refs)
	}
}

func TestResourceRegistryPlansChinesePromptWithoutSpaces(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "source.pptx"), []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "output.pptx"), []byte("old output"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewResourceRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := registry.PlanRequestInputs(context.Background(), workcontract.RequestInputRequest{Prompt: "请把docs/source.pptx修改后另存为output.pptx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Path != "docs/source.pptx" {
		t.Fatalf("planned refs=%+v", refs)
	}
}

func TestResourceRegistryTreatsNamedExistingFileAsOutput(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ultra5-comparison-summary.md"), []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "2026笔记本选型比较.pptx"), []byte("existing output"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewResourceRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	prompt := "根据 ultra5-comparison-summary.md，写一个PPT文件，主题色为红色，文件名称为 2026笔记本选型比较.pptx"
	refs, err := registry.PlanRequestInputs(context.Background(), workcontract.RequestInputRequest{Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Path != "ultra5-comparison-summary.md" {
		t.Fatalf("named output must not be staged as input: %+v", refs)
	}
}

func resourceRegistryContext(root string) context.Context {
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", RunID: "run"}}
	execution := workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: root, InputDir: filepath.Join(root, "input"), OutputDir: filepath.Join(root, "output"), TmpDir: filepath.Join(root, "tmp")}}
	prepared := workmodel.PreparedRun{Manifest: workmodel.RunManifest{RunID: "run", Scope: workmodel.ResourceScope{TenantID: "tenant"}}, Execution: execution}
	return workcontract.WithPreparedRun(context.Background(), prepared)
}
