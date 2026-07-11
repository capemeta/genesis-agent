package pathresolver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/platform/contextutil"
)

func TestResolverMarksEscapeAsExternal(t *testing.T) {
	root := tempWorkspace(t)
	resolver, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolver.Resolve(context.Background(), model.PathRef{Raw: filepath.Join(root, "..", "outside.txt")}, fscontract.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != model.PathScopeExternal {
		t.Fatalf("Scope = %q, want external", got.Scope)
	}
}

func TestResolverResolvesRelativePath(t *testing.T) {
	root := tempWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolver.Resolve(context.Background(), model.PathRef{Raw: "a.txt"}, fscontract.ResolveOptions{MustExist: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceRel != "a.txt" {
		t.Fatalf("WorkspaceRel = %q, want a.txt", got.WorkspaceRel)
	}
}

func tempWorkspace(t *testing.T) string {
	t.Helper()
	base := filepath.Join("D:\\workspace\\go\\genesis-agent", ".testdata")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(base, ".resolver-test-*")
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(abs) })
	return abs
}

func TestResolverRejectsDirectoryWhenFileExpected(t *testing.T) {
	root := tempWorkspace(t)
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolver, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = resolver.Resolve(context.Background(), model.PathRef{Raw: "dir"}, fscontract.ResolveOptions{MustExist: true})
	if fscontract.CodeOf(err) != fscontract.ErrCodeInvalidInput {
		t.Fatalf("Resolve error code = %q, want invalid_input", fscontract.CodeOf(err))
	}
}

func TestResolverRequiresDirectory(t *testing.T) {
	root := tempWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = resolver.Resolve(context.Background(), model.PathRef{Raw: "a.txt"}, fscontract.ResolveOptions{
		MustExist:        true,
		AllowDirectory:   true,
		RequireDirectory: true,
	})
	if fscontract.CodeOf(err) != fscontract.ErrCodeNotDirectory {
		t.Fatalf("Resolve error code = %q, want not_directory", fscontract.CodeOf(err))
	}
}

func TestResolverMarksProtectedPath(t *testing.T) {
	root := tempWorkspace(t)
	resolver, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolver.Resolve(context.Background(), model.PathRef{Raw: `C:\Windows\System32\config\missing.txt`}, fscontract.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != model.PathScopeProtected {
		t.Fatalf("Scope = %q, want protected", got.Scope)
	}
}

func TestResolverMarksSSHDirectoryItselfProtected(t *testing.T) {
	root := tempWorkspace(t)
	resolver, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolver.Resolve(context.Background(), model.PathRef{Raw: `C:\Users\dev\.ssh`}, fscontract.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != model.PathScopeProtected {
		t.Fatalf("Scope = %q, want protected", got.Scope)
	}
}

func TestResolverCanPreserveFinalSymlink(t *testing.T) {
	root := tempWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(filepath.Join(root, "target.txt"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	resolver, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolver.Resolve(context.Background(), model.PathRef{Raw: "link.txt"}, fscontract.ResolveOptions{MustExist: true, PreserveFinalSymlink: true})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got.BackendPath) != filepath.Clean(link) {
		t.Fatalf("BackendPath=%q, want link path %q", got.BackendPath, link)
	}
}

func TestResolverExpandsWorkDirLogicalPath(t *testing.T) {
	root := tempWorkspace(t)
	resolver, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "run-test-1")
	got, err := resolver.Resolve(ctx, model.PathRef{Raw: "$WORK_DIR/deck_gen.js"}, fscontract.ResolveOptions{MustExist: false})
	if err != nil {
		t.Fatal(err)
	}
	wantRel := ".genesis/runs/run-test-1/work/deck_gen.js"
	if got.WorkspaceRel != wantRel {
		t.Fatalf("WorkspaceRel=%q, want %q", got.WorkspaceRel, wantRel)
	}
	wantAbs := filepath.Join(root, ".genesis", "runs", "run-test-1", "work", "deck_gen.js")
	if filepath.Clean(got.BackendPath) != filepath.Clean(wantAbs) {
		t.Fatalf("BackendPath=%q, want %q", got.BackendPath, wantAbs)
	}
	if _, err := os.Stat(filepath.Dir(got.BackendPath)); err != nil {
		t.Fatalf("work dir should exist after resolve: %v", err)
	}
}

func TestResolverLogicalPathRequiresRunID(t *testing.T) {
	root := tempWorkspace(t)
	resolver, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(context.Background(), model.PathRef{Raw: "$WORK_DIR/deck_gen.js"}, fscontract.ResolveOptions{})
	if err == nil {
		t.Fatal("expected error without run_id")
	}
}
