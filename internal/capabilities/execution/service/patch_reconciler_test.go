package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspacePatchReconciler(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "patch_reconciler_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	reconciler := NewWorkspacePatchReconciler()

	// 测试用例 1: 正常在 baseDir 内 apply 单文件补丁
	patch := WorkspacePatch{
		Summary: "Refactored main.py via subagent",
		Files: []FilePatch{
			{
				RelativePath: "src/main.py",
				NewContent:   "print('Hello Refactored')",
			},
		},
	}

	if err := reconciler.ApplyPatch(tmpDir, patch); err != nil {
		t.Fatalf("unexpected ApplyPatch error: %v", err)
	}

	targetFile := filepath.Join(tmpDir, "src", "main.py")
	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed to read patched file: %v", err)
	}
	if string(content) != "print('Hello Refactored')" {
		t.Errorf("patched content mismatch, got: %s", string(content))
	}

	// 测试用例 2: 防范越权目录穿透 `../evil.py`
	traversalPatch := WorkspacePatch{
		Files: []FilePatch{
			{
				RelativePath: "../evil.py",
				NewContent:   "evil",
			},
		},
	}
	if err := reconciler.ApplyPatch(tmpDir, traversalPatch); err == nil {
		t.Fatalf("expected path traversal error, got nil")
	}
}
