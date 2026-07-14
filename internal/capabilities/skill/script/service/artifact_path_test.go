package service

import (
	"path/filepath"
	"testing"

	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
)

func TestArtifactPathForModelRelativizesUnderWorkspace(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, ".genesis", "runs", "r1", "output", "office-ppt", "deck.pptx")
	got := pathForModel(root, abs)
	want := ".genesis/runs/r1/output/office-ppt/deck.pptx"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestArtifactPathForModelKeepsAlreadyRelative(t *testing.T) {
	got := pathForModel(t.TempDir(), `.genesis/runs/r1/output/a.pptx`)
	if got != ".genesis/runs/r1/output/a.pptx" {
		t.Fatalf("got=%q", got)
	}
}

func TestArtifactPathForModelOutsideWorkspaceKeepsAbs(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "leak.pptx")
	got := pathForModel(root, outside)
	if !filepath.IsAbs(filepath.FromSlash(got)) {
		t.Fatalf("outside workspace should keep absolute fallback, got=%q", got)
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
	abs := filepath.Join(root, ".genesis", "runs", "r1", "work", "skills", "demo")
	got := projectHostWorkDirsForModel(root, abs)
	want := ".genesis/runs/r1/work/skills/demo"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestProjectArtifactsForModel(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, ".genesis", "runs", "r1", "output", "demo", "a.txt")
	arts := projectArtifactsForModel(root, []scriptcontract.Artifact{{Name: "a.txt", Path: abs, OK: true}})
	if arts[0].Path != ".genesis/runs/r1/output/demo/a.txt" {
		t.Fatalf("%+v", arts[0])
	}
	if arts[0].Name != "a.txt" || !arts[0].OK {
		t.Fatalf("%+v", arts[0])
	}
}
