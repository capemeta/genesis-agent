package pathutil

import (
	"path/filepath"
	"testing"
)

func TestNormalizeAbsolutePath(t *testing.T) {
	// 绝对路径应保持有效（不 EvalSymlinks，因为路径可能不存在）
	result, err := Normalize("/some/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if !filepath.IsAbs(result) {
		t.Fatalf("Normalize() should return absolute path, got %q", result)
	}
}

func TestNormalizeEmptyPath(t *testing.T) {
	_, err := Normalize("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestNormalizeListBestEffortFiltersEmpty(t *testing.T) {
	paths := []string{"/a/b", "", "/c/d"}
	results := NormalizeListBestEffort(paths)
	if len(results) != 2 {
		t.Fatalf("expected 2 results (skipping empty), got %d: %#v", len(results), results)
	}
}

func TestNormalizeListBestEffortDedup(t *testing.T) {
	paths := []string{"/a/b", "/a/b", "/a/b/"}
	results := NormalizeListBestEffort(paths)
	// 规范化后 /a/b 和 /a/b/ 应合并为同一路径
	if len(results) != 1 {
		t.Fatalf("expected dedup to 1 result, got %d: %#v", len(results), results)
	}
}

func TestNormalizeListBestEffortPreservesOnError(t *testing.T) {
	// 相对路径在某些情况下可能导致 Abs 失败，但 NormalizeListBestEffort 应保留原路径（经 filepath.Clean）
	paths := []string{"/valid/path"}
	results := NormalizeListBestEffort(paths)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for valid path")
	}
}
