package model

import (
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"testing"
)

func TestNewSourceProvenanceRecordsGitHubAddressAndDomain(t *testing.T) {
	record := MarketplaceRecord{
		Name: "anthropic-agent-skills",
		Source: MarketplaceSource{
			Type:    SourceTypeGitHub,
			Repo:    "anthropics/skills",
			Ref:     "v1.0.0",
			SubPath: "skills",
		},
	}
	pkg := Package{Name: "document-skills", Type: PackageTypeSkillPackage, Source: "./", Capabilities: []capmodel.CapabilityManifest{{Type: capmodel.CapabilityTypeSkill, Name: "pdf", Path: "./skills/pdf"}}}

	provenance := NewSourceProvenance(record, pkg, "abc123", "hash")

	if provenance.Address != "https://github.com/anthropics/skills@v1.0.0#skills" {
		t.Fatalf("address = %q", provenance.Address)
	}
	if provenance.Domain != "github.com" {
		t.Fatalf("domain = %q", provenance.Domain)
	}
	if provenance.PackageSource != "./" || provenance.ResolvedRevision != "abc123" || provenance.ContentHash != "hash" {
		t.Fatalf("unexpected provenance: %+v", provenance)
	}
}

func TestSourceDomainFromURL(t *testing.T) {
	source := MarketplaceSource{Type: SourceTypeURL, URL: "https://Example.COM/plugins/skills.zip"}
	if got := SourceDomain(source); got != "example.com" {
		t.Fatalf("domain = %q", got)
	}
}
