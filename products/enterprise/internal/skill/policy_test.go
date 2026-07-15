package skill

import (
	"context"
	"testing"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

func TestDefaultAllowedSourcePolicyDeniesRemote(t *testing.T) {
	p := DefaultAllowedSourcePolicy()
	err := p.Check(context.Background(), marketmodel.MarketplaceSource{
		Type: marketmodel.SourceTypeGitHub,
		Repo: "org/repo",
	}, "enterprise")
	if err == nil {
		t.Fatal("expected deny")
	}
}
