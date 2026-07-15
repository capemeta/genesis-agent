package policy_test

import (
	"context"
	"testing"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
	marketpolicy "genesis-agent/internal/capabilities/package/marketplace/policy"
)

func TestAllowHostsExtraDomain(t *testing.T) {
	p := marketpolicy.AllowHosts{
		Hosts:      []string{"github.com", "git.example.com"},
		AllowLocal: true,
	}
	if err := p.Check(context.Background(), marketmodel.MarketplaceSource{
		Type: marketmodel.SourceTypeGitHub,
		Host: "git.example.com",
		Repo: "org/repo",
	}, "cli"); err != nil {
		t.Fatal(err)
	}
	if err := p.Check(context.Background(), marketmodel.MarketplaceSource{
		Type: marketmodel.SourceTypeURL,
		URL:  "https://evil.example/pack.zip",
	}, "cli"); err == nil {
		t.Fatal("expected deny")
	}
}

func TestAllowHostsDownloadAPIURL(t *testing.T) {
	p := marketpolicy.AllowHosts{
		Hosts:      []string{"github.com", "openskills.cc"},
		AllowLocal: true,
	}
	if err := p.Check(context.Background(), marketmodel.MarketplaceSource{
		Type: marketmodel.SourceTypeURL,
		URL:  "https://openskills.cc/api/download?slug=frontend-design&locale=zh",
	}, "cli"); err != nil {
		t.Fatal(err)
	}
}
