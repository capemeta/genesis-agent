package service

import (
	"os"
	"path/filepath"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func TestMaterializeResultArtifactsCopiesExternalArtifactToOutputDir(t *testing.T) {
	outputDir := t.TempDir()
	artifactStore := t.TempDir()
	source := filepath.Join(artifactStore, "remote.txt")
	if err := os.WriteFile(source, []byte("remote"), 0o644); err != nil {
		t.Fatal(err)
	}

	artifacts, warnings := materializeResultArtifacts(outputDir, []execmodel.Artifact{{Name: "remote.txt", LocalPath: source}})
	if len(warnings) != 0 {
		t.Fatalf("warnings=%v", warnings)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts=%+v", artifacts)
	}
	if filepath.Clean(filepath.Dir(artifacts[0].Path)) != filepath.Clean(outputDir) {
		t.Fatalf("artifact should be materialized under output dir: %+v", artifacts[0])
	}
	if _, err := os.Stat(filepath.Join(outputDir, "remote.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestMaterializeResultArtifactsKeepsArtifactAlreadyInOutputDir(t *testing.T) {
	outputDir := t.TempDir()
	source := filepath.Join(outputDir, "actual.txt")
	if err := os.WriteFile(source, []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}

	artifacts, warnings := materializeResultArtifacts(outputDir, []execmodel.Artifact{{Name: "reported.txt", LocalPath: source}})
	if len(warnings) != 0 {
		t.Fatalf("warnings=%v", warnings)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts=%+v", artifacts)
	}
	if filepath.Clean(artifacts[0].Path) != filepath.Clean(source) {
		t.Fatalf("artifact inside output dir should not be copied or renamed: %+v", artifacts[0])
	}
	if _, err := os.Stat(filepath.Join(outputDir, "reported.txt")); !os.IsNotExist(err) {
		t.Fatalf("unexpected copied artifact, err=%v", err)
	}
}
