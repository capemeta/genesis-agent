package subagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"genesis-agent/internal/runtime/multiagent/model"
)

func TestWorkspaceResourcesValidateAndProjectSafeArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "out"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "out", "report.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	resources, err := NewWorkspaceResources(root)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := resources.Validate(context.Background(), model.ArtifactManifest{Artifacts: []model.Artifact{
		{Path: "out/report.txt", Kind: "file"},
		{Path: "../secret.txt", Kind: "file"},
	}}, []model.Finding{{Claim: "done", Evidence: []string{"out/report.txt", "../secret.txt"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(validated.Artifacts) != 1 || validated.Artifacts[0].ResourceID == "" || validated.Artifacts[0].Path != "out/report.txt" {
		t.Fatalf("unexpected artifacts: %+v", validated.Artifacts)
	}
	if filepath.IsAbs(validated.Artifacts[0].Path) || strings.Contains(validated.Artifacts[0].Path, "\\") {
		t.Fatalf("artifact path must be workspace-relative slash path: %+v", validated.Artifacts[0])
	}
	if len(validated.Findings) != 1 || len(validated.Findings[0].Evidence) != 1 || validated.Findings[0].Evidence[0] != "out/report.txt" {
		t.Fatalf("unsafe evidence was not filtered: %+v", validated.Findings)
	}
	projected, ok, err := resources.ProjectArtifact(context.Background(), validated.Artifacts[0])
	if err != nil || !ok {
		t.Fatalf("project failed ok=%v err=%v", ok, err)
	}
	if projected.ContentHash == "" || projected.Path != "out/report.txt" {
		t.Fatalf("unexpected projected artifact: %+v", projected)
	}
}

func TestWorkspaceResourcesRejectsHashMismatchAtProjection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	resources, err := NewWorkspaceResources(root)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := resources.Validate(context.Background(), model.ArtifactManifest{Artifacts: []model.Artifact{{Path: "report.txt"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := resources.ProjectArtifact(context.Background(), validated.Artifacts[0]); err != nil || ok {
		t.Fatalf("expected hash mismatch rejection ok=%v err=%v", ok, err)
	}
}

func TestWorkspaceResourcesRewritesUnsafeResourceID(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	resources, err := NewWorkspaceResources(root)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := resources.Validate(context.Background(), model.ArtifactManifest{Artifacts: []model.Artifact{{ResourceID: "D:/secret/token", Path: "report.txt"}}}, []model.Finding{{Claim: "done", Evidence: []string{"D:/secret/token", "report.txt"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(validated.Artifacts) != 1 || !strings.HasPrefix(validated.Artifacts[0].ResourceID, "res-") {
		t.Fatalf("unsafe resource id was not rewritten: %+v", validated.Artifacts)
	}
	if strings.Contains(validated.Artifacts[0].ResourceID, ":") || strings.Contains(validated.Artifacts[0].ResourceID, "/") {
		t.Fatalf("unsafe resource id leaked: %+v", validated.Artifacts[0])
	}
	if len(validated.Findings) != 1 || validated.Findings[0].Evidence[0] != "report.txt" {
		t.Fatalf("unsafe evidence id should be filtered: %+v", validated.Findings)
	}
}

// TestWorkspaceResourcesPreservesQAAssetRole 回归：证据校验重建 Artifact 时必须保留 Role，
// 否则父侧归约无法剔除 qa_asset，会把缩略图泄漏给父 Agent。
func TestWorkspaceResourcesPreservesQAAssetRole(t *testing.T) {
	resources, err := NewWorkspaceResources(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	validated, err := resources.Validate(context.Background(), model.ArtifactManifest{Artifacts: []model.Artifact{
		{CandidateID: "produced-qa", ResourceID: "produced-qa", Name: "slide-1.jpg", Kind: "file", Role: model.ArtifactRoleQAAsset},
		{CandidateID: "produced-deck", ResourceID: "produced-deck", Name: "deck.pptx", Kind: "file"},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(validated.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %+v", validated.Artifacts)
	}
	byID := map[string]model.Artifact{}
	for _, art := range validated.Artifacts {
		byID[art.CandidateID] = art
	}
	if byID["produced-qa"].Role != model.ArtifactRoleQAAsset {
		t.Fatalf("qa_asset Role was dropped: %+v", byID["produced-qa"])
	}
	if byID["produced-deck"].Role != "" {
		t.Fatalf("deliverable Role should stay empty: %+v", byID["produced-deck"])
	}
}
