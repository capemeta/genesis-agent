package service

import (
	"path/filepath"
	"testing"
)

func TestArtifactPathForModelRelativizesUnderWorkspace(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, ".genesis", "runtime", "runs", "r1", "output", "office-ppt", "deck.pptx")
	got := pathForModel(root, abs)
	want := ".genesis/runtime/runs/r1/output/office-ppt/deck.pptx"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestArtifactPathForModelKeepsAlreadyRelative(t *testing.T) {
	got := pathForModel(t.TempDir(), `.genesis/runtime/runs/r1/output/a.pptx`)
	if got != ".genesis/runtime/runs/r1/output/a.pptx" {
		t.Fatalf("got=%q", got)
	}
}

func TestPathForModelOutsideWorkspaceDoesNotLeakAbsolutePath(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "leak.pptx")
	got := pathForModel(root, outside)
	if got != "" {
		t.Fatalf("outside workspace leaked path: %q", got)
	}
}

func TestPathForModelKeepsRemoteSandboxNamespace(t *testing.T) {
	root := t.TempDir()
	if got := pathForModel(root, "/workspace"); got != "/workspace" {
		t.Fatalf("got=%q", got)
	}
	if got := pathForModel(root, "/workspace/skills/office-ppt"); got != "/workspace/skills/office-ppt" {
		t.Fatalf("got=%q", got)
	}
}

func TestProjectHostWorkDirsForModel(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, ".genesis", "runtime", "runs", "r1", "work", "skills", "demo")
	got := projectHostWorkDirsForModel(root, abs)
	want := ".genesis/runtime/runs/r1/work/skills/demo"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}
