package fs_backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
)

func TestBackendWriteReadAndList(t *testing.T) {
	root := t.TempDir()
	path := resolved(root, "dir/a.txt")
	backend := New()

	if err := backend.Write(context.Background(), path, []byte("hello"), fscontract.WriteOptions{CreateParents: true, Overwrite: true, Atomic: true}); err != nil {
		t.Fatal(err)
	}
	data, err := backend.Read(context.Background(), path, fscontract.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("Read = %q, want hello", data)
	}
	entries, err := backend.ListDir(context.Background(), resolved(root, "dir"), fscontract.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "a.txt" {
		t.Fatalf("entries = %#v, want a.txt", entries)
	}
}

func TestBackendOverwriteFalse(t *testing.T) {
	root := t.TempDir()
	path := resolved(root, "a.txt")
	if err := os.WriteFile(path.BackendPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := New().Write(context.Background(), path, []byte("new"), fscontract.WriteOptions{Overwrite: false})
	if fscontract.CodeOf(err) != fscontract.ErrCodeAlreadyExists {
		t.Fatalf("Write error code = %q, want already_exists", fscontract.CodeOf(err))
	}
}

func TestBackendWalkIsBounded(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := New().Walk(context.Background(), resolved(root, ""), fscontract.WalkOptions{MaxEntries: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Truncated {
		t.Fatal("Walk Truncated = false, want true")
	}
}

func resolved(root string, rel string) model.ResolvedPath {
	return model.ResolvedPath{
		DisplayPath:  filepath.ToSlash(rel),
		BackendPath:  filepath.Join(root, rel),
		WorkspaceRel: filepath.ToSlash(rel),
		WorkspaceID:  "test",
	}
}

func TestBackendExpectedHashMismatch(t *testing.T) {
	root := t.TempDir()
	path := resolved(root, "a.txt")
	if err := os.WriteFile(path.BackendPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := New().Write(context.Background(), path, []byte("new"), fscontract.WriteOptions{Overwrite: true, ExpectedHash: "bad"})
	if fscontract.CodeOf(err) != fscontract.ErrCodeModifiedExternally {
		t.Fatalf("Write error code = %q, want modified", fscontract.CodeOf(err))
	}
}

func TestBackendWalkExcludeDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "hidden.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "wanted.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := New().Walk(context.Background(), resolved(root, ""), fscontract.WalkOptions{ExcludeDirs: []string{"node_modules"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range out.Entries {
		if strings.Contains(entry.Path, "node_modules") {
			t.Fatalf("excluded directory leaked into walk: %+v", entry)
		}
	}
}
func TestBackendWalkRejectsFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	_, err := New().Walk(context.Background(), resolved(root, ""), fscontract.WalkOptions{FollowSymlinks: true})
	if fscontract.CodeOf(err) != fscontract.ErrCodeInvalidInput {
		t.Fatalf("Walk error code = %q, want invalid_input", fscontract.CodeOf(err))
	}
}

func TestBackendRemoveDirectoryRequiresRecursive(t *testing.T) {
	root := t.TempDir()
	dir := resolved(root, "dir")
	if err := os.MkdirAll(dir.BackendPath, 0o755); err != nil {
		t.Fatal(err)
	}
	err := New().Remove(context.Background(), dir, fscontract.RemoveOptions{})
	if fscontract.CodeOf(err) != fscontract.ErrCodeInvalidInput {
		t.Fatalf("Remove error code = %q, want invalid_input", fscontract.CodeOf(err))
	}
	if err := New().Remove(context.Background(), dir, fscontract.RemoveOptions{Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir.BackendPath); !os.IsNotExist(err) {
		t.Fatalf("dir still exists err=%v", err)
	}
}
func TestBackendWalkRejectsExcessiveLimits(t *testing.T) {
	root := t.TempDir()
	_, err := New().Walk(context.Background(), resolved(root, ""), fscontract.WalkOptions{MaxDepth: 65})
	if fscontract.CodeOf(err) != fscontract.ErrCodeInvalidInput {
		t.Fatalf("Walk error code = %q, want invalid_input", fscontract.CodeOf(err))
	}
}

func TestBackendReadRejectsExcessiveLimit(t *testing.T) {
	root := t.TempDir()
	path := resolved(root, "a.txt")
	if err := os.WriteFile(path.BackendPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New().Read(context.Background(), path, fscontract.ReadOptions{MaxBytes: maxReadBytes + 1})
	if fscontract.CodeOf(err) != fscontract.ErrCodeInvalidInput {
		t.Fatalf("Read error code = %q, want invalid_input", fscontract.CodeOf(err))
	}
}

func TestBackendListDirSortsBeforeTruncating(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := New().ListDir(context.Background(), resolved(root, ""), fscontract.ListOptions{MaxEntries: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "a.txt" {
		t.Fatalf("entries = %#v, want first sorted entry a.txt", entries)
	}
}

func TestBackendListDirRejectsExcessiveLimit(t *testing.T) {
	root := t.TempDir()
	_, err := New().ListDir(context.Background(), resolved(root, ""), fscontract.ListOptions{MaxEntries: maxListEntries + 1})
	if fscontract.CodeOf(err) != fscontract.ErrCodeInvalidInput {
		t.Fatalf("ListDir error code = %q, want invalid_input", fscontract.CodeOf(err))
	}
}
