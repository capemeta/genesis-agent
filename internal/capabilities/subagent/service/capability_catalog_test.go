package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	capmodel "genesis-agent/internal/capabilities/capability/model"
)

type fakeCapabilityRegistry struct {
	records []capmodel.CapabilityIndexRecord
}

func (r fakeCapabilityRegistry) ListCapabilities(_ context.Context, query capmodel.CapabilityQuery) ([]capmodel.CapabilityIndexRecord, error) {
	var records []capmodel.CapabilityIndexRecord
	for _, record := range r.records {
		if !record.Enabled || record.Type != capmodel.CapabilityTypeSubAgent {
			continue
		}
		if query.Product != "" && len(record.Products) > 0 && record.Products[0] != query.Product {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func (fakeCapabilityRegistry) SetCapabilityEnabled(context.Context, string, bool) (capmodel.CapabilityIndexRecord, error) {
	return capmodel.CapabilityIndexRecord{}, nil
}

func TestLoadCapabilityDefinitionsLoadsEnabledProductDefinition(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agents", "review.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: marketplace-review\ndescription: review code\n---\nReview the change."), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := fakeCapabilityRegistry{records: []capmodel.CapabilityIndexRecord{{
		ID: "marketplace.review", Type: capmodel.CapabilityTypeSubAgent, Enabled: true,
		InstallRoot: root, ResourcePath: filepath.Join("agents", "review.md"), Products: []string{"cli"},
	}}}
	definitions, err := LoadCapabilityDefinitions(context.Background(), registry, "cli")
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || definitions[0].Name != "marketplace-review" {
		t.Fatalf("definitions = %#v", definitions)
	}
}

func TestLoadCapabilityDefinitionsRejectsPathEscape(t *testing.T) {
	registry := fakeCapabilityRegistry{records: []capmodel.CapabilityIndexRecord{{
		ID: "marketplace.escape", Type: capmodel.CapabilityTypeSubAgent, Enabled: true,
		InstallRoot: t.TempDir(), ResourcePath: "..\\outside.md",
	}}}
	if _, err := LoadCapabilityDefinitions(context.Background(), registry, "cli"); err == nil {
		t.Fatal("expected path escape error")
	}
}

func TestLoadCapabilityDefinitionsRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("---\nname: outside\ndescription: outside\n---\noutside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境不允许创建符号链接: %v", err)
	}
	registry := fakeCapabilityRegistry{records: []capmodel.CapabilityIndexRecord{{
		ID: "marketplace.symlink", Type: capmodel.CapabilityTypeSubAgent, Enabled: true,
		InstallRoot: root, ResourcePath: "linked.md",
	}}}
	if _, err := LoadCapabilityDefinitions(context.Background(), registry, "cli"); err == nil {
		t.Fatal("expected symlink escape error")
	}
}
