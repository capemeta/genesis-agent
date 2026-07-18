package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestPublishThenMaterializeKeepsArtifactIndependentFromRun(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "work", "summary.md")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("# Summary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	sha := hex.EncodeToString(digest[:])
	staged, err := store.Stage(context.Background(), "artifact-1", "summary.md", bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.Commit(context.Background(), staged, artifactmodel.Manifest{ArtifactRef: artifactmodel.ArtifactRef{ID: "artifact-1", Name: "summary.md", Kind: "md", Size: int64(len(content)), SHA256: sha, MIME: "text/markdown", Producer: "test", RunID: "run-1", Scope: workmodel.ResourceScope{TenantID: "tenant"}}, GateVersion: "test/v1", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.ID != "artifact-1" || artifact.SHA256 == "" || artifact.StorageRef.Path == "" {
		t.Fatalf("artifact = %+v", artifact)
	}
	recovered, ok, err := store.GetCommitted(context.Background(), artifact.ID)
	if err != nil || !ok || recovered.ID != artifact.ID || recovered.SHA256 != artifact.SHA256 {
		t.Fatalf("GetCommitted = %+v, %v, %v", recovered, ok, err)
	}
	deliveryDir := filepath.Join(root, "project")
	if err := os.MkdirAll(deliveryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	targets, _ := NewTargetRegistry(map[string]string{"project": deliveryDir})
	materializer, _ := NewMaterializer(store, targets)
	result, err := materializer.Materialize(context.Background(), artifact, artifactmodel.DeliveryTarget{Kind: artifactmodel.DeliveryProjectRoot, Resource: workmodel.ResourceRef{Authority: "host", Scheme: "project", ID: "project"}, Name: "summary.md"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Display != filepath.Join(deliveryDir, "summary.md") {
		t.Fatalf("display = %q", result.Display)
	}
	recoveredDelivery, ok, err := materializer.GetMaterialized(context.Background(), artifact, result.Target)
	if err != nil || !ok || recoveredDelivery.Resource.Version != "sha256:"+artifact.SHA256 {
		t.Fatalf("GetMaterialized = %+v, %v, %v", recoveredDelivery, ok, err)
	}
	if err := os.WriteFile(result.Display, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := materializer.GetMaterialized(context.Background(), artifact, result.Target); err != nil || ok {
		t.Fatalf("tampered target must not recover: ok=%v err=%v", ok, err)
	}
	if err := os.RemoveAll(filepath.Join(root, "work")); err != nil {
		t.Fatal(err)
	}
	reader, err := store.Open(context.Background(), artifact)
	if err != nil {
		t.Fatalf("artifact disappeared with run work: %v", err)
	}
	_ = reader.Close()
}

func TestMaterializeConflictPreservesArtifactReference(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(filepath.Join(root, "state"))
	dir := filepath.Join(root, "project")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "report.md"), []byte("existing"), 0o600)
	targets, _ := NewTargetRegistry(map[string]string{"project": dir})
	materializer, _ := NewMaterializer(store, targets)
	artifact := artifactmodel.ArtifactRef{ID: "artifact-1", Name: "report.md", SHA256: "hash"}
	_, err := materializer.Materialize(context.Background(), artifact, artifactmodel.DeliveryTarget{Kind: artifactmodel.DeliveryProjectRoot, Resource: workmodel.ResourceRef{Authority: "host", ID: "project"}, Name: "report.md"})
	artifactErr, ok := err.(*artifactcontract.Error)
	if !ok || artifactErr.Code != artifactcontract.ErrCodeDeliveryTargetConflict || artifactErr.Artifact == nil {
		t.Fatalf("error = %#v", err)
	}
}

func TestReplaceMaterializeOverwritesSameTargetAtomically(t *testing.T) {
	root := t.TempDir()
	store, _ := NewStore(filepath.Join(root, "state"))
	dir := filepath.Join(root, "out")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "deck.pptx"), []byte("old-bytes"), 0o600)
	content := []byte("new-bytes")
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	object, err := store.Stage(context.Background(), "artifact-2", "deck.pptx", bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.Commit(context.Background(), object, artifactmodel.Manifest{
		ArtifactRef: artifactmodel.ArtifactRef{ID: "artifact-2", Name: "deck.pptx", Kind: "pptx", Size: int64(len(content)), SHA256: sha, MIME: "application/pptx", Producer: "test", RunID: "run-1", Scope: workmodel.ResourceScope{TenantID: "tenant"}},
		GateVersion: "test/v1", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	targets, _ := NewTargetRegistry(map[string]string{"out": dir})
	materializer, _ := NewMaterializer(store, targets)
	target := artifactmodel.DeliveryTarget{Kind: artifactmodel.DeliveryProjectRoot, Resource: workmodel.ResourceRef{Authority: "host", ID: "out"}, Name: "deck.pptx"}
	if _, err := materializer.Materialize(context.Background(), artifact, target); err == nil {
		t.Fatal("expected conflict without replace")
	}
	result, err := materializer.ReplaceMaterialize(context.Background(), artifact, target)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "deck.pptx"))
	if err != nil || string(data) != "new-bytes" {
		t.Fatalf("replaced content=%q err=%v display=%s", data, err, result.Display)
	}
}
