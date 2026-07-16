package result

import (
	"context"
	"testing"

	"genesis-agent/internal/runtime/multiagent/model"
)

func TestManifestRegistrySnapshotsIndependentCopies(t *testing.T) {
	registry := NewManifestRegistry()
	ctx := WithManifestRegistry(context.Background(), registry)
	if !RegisterArtifact(ctx, model.Artifact{ResourceID: "res-1", Kind: "file"}) {
		t.Fatal("expected artifact registration")
	}
	if !RegisterFinding(ctx, model.Finding{Claim: "done", Evidence: []string{"res-1"}}) {
		t.Fatal("expected finding registration")
	}
	manifest, findings := registry.Snapshot()
	manifest.Artifacts[0].ResourceID = "mutated"
	findings[0].Evidence[0] = "mutated"
	again, againFindings := registry.Snapshot()
	if again.Artifacts[0].ResourceID != "res-1" || againFindings[0].Evidence[0] != "res-1" {
		t.Fatalf("snapshot mutated registry: %+v %+v", again, againFindings)
	}
}

func TestManifestRegistryRejectsUnboundContext(t *testing.T) {
	if RegisterArtifact(context.Background(), model.Artifact{ResourceID: "res-1"}) {
		t.Fatal("unbound context must not register artifact")
	}
}
