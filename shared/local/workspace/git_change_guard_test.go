package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestGitChangeGuardComparesAgainstDirtyRunBaseline(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "Test")
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "main.go")
	runGitTest(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(file, []byte("package main\n// user dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared := projectChangePrepared(root)
	guard := NewGitChangeGuard()
	if err := guard.InitializeRun(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	decision, err := guard.EvaluateCompletion(context.Background(), prepared)
	if err != nil || decision.Complete {
		t.Fatalf("existing dirty state must not satisfy run delta: decision=%+v err=%v", decision, err)
	}
	if err := os.WriteFile(file, []byte("package main\n// user dirty\n// agent delta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	decision, err = guard.EvaluateCompletion(context.Background(), prepared)
	if err != nil || !decision.Complete {
		t.Fatalf("agent delta should satisfy gate: decision=%+v err=%v", decision, err)
	}
	guard.ReleaseRun(prepared)
	if _, ok := guard.baselines[prepared.Manifest.RunID]; ok {
		t.Fatal("ReleaseRun() did not remove baseline")
	}
}

func projectChangePrepared(root string) workmodel.PreparedRun {
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run"}}
	return workmodel.PreparedRun{Manifest: workmodel.RunManifest{RunID: "run", ProjectDir: root, ProjectChangeRequired: true}, Execution: workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: root}}}
}

func runGitTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
