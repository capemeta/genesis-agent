package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
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
	refs, err := registry.ResolveInputs(context.Background(), []string{"report.md"})
	if err != nil || len(refs) != 1 {
		t.Fatalf("resolve refs=%v err=%v", refs, err)
	}
	if err := os.WriteFile(path, []byte("changed after approval"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = registry.Open(context.Background(), refs[0])
	var classified *workcontract.Error
	if !errors.As(err, &classified) || classified.Code != workcontract.ErrCodeResourceVersionConflict {
		t.Fatalf("应返回版本冲突，实际: %v", err)
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
	if _, err := registry.ResolveInputs(context.Background(), []string{"../secret.txt"}); err == nil {
		t.Fatal("相对路径不得越过 project root")
	}
}
