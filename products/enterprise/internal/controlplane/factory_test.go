package controlplane_test

import (
	"strings"
	"testing"

	enterprisecontrolplane "genesis-agent/products/enterprise/internal/controlplane"
)

func TestBuildTenantDependenciesRequiresStateRoot(t *testing.T) {
	_, err := enterprisecontrolplane.BuildTenantDependencies(enterprisecontrolplane.Options{})
	if err == nil || !strings.Contains(err.Error(), "StateRoot") {
		t.Fatalf("expected StateRoot error, got %v", err)
	}
}

func TestBuildTenantDependenciesAssemblesControlPlane(t *testing.T) {
	deps, err := enterprisecontrolplane.BuildTenantDependencies(enterprisecontrolplane.Options{
		TenantStateRoot: t.TempDir(),
		DeliveryRoot:    t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if deps.RunManifests == nil || deps.ProducedResources == nil || deps.RemoteSessions == nil || deps.Reservations == nil || deps.Deliverables == nil ||
		deps.ArtifactRuns == nil || deps.Finalizer == nil || deps.Completion == nil || deps.QAEvidence == nil || deps.Adoptions == nil || deps.SkillBindings == nil || deps.SkillPackages == nil || deps.SubAgentStore == nil {
		t.Fatalf("incomplete dependencies: %+v", deps)
	}
}
