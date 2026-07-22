package profile

import (
	"context"
	"strings"
	"testing"

	"genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
)

// TestEmbeddedOfficeAllowedToolsAlignWithCLIProfile 保证内置 Office/PDF skill 的
// allowed-tools 不会声明 CLI 默认 Profile 未启用的工具（避免加载后求交失败）。
func TestEmbeddedOfficeAllowedToolsAlignWithCLIProfile(t *testing.T) {
	prof := DefaultProfile(false)
	enabled := map[string]struct{}{}
	for _, name := range prof.Tools.Enabled {
		enabled[strings.TrimSpace(name)] = struct{}{}
	}
	systemFS, err := embedded.SystemFS()
	if err != nil {
		t.Fatal(err)
	}
	source, err := embedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, skillmodel.ScopeSystem, systemFS, skillparser.New())
	if err != nil {
		t.Fatal(err)
	}
	listed, err := source.List(context.Background(), skillcontract.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, physical := range listed.Packages {
		switch physical.Metadata.Name {
		case "office-ppt", "office-word", "office-excel", "office-pdf":
		default:
			continue
		}
		checked++
		if physical.Manifest == nil {
			t.Fatalf("skill %s missing genesis.skill.yaml", physical.Metadata.Name)
		}
		for _, invocation := range physical.Manifest.Invocations {
			for _, allowed := range invocation.ToolPolicy.Allow {
				norm := strings.TrimSpace(allowed)
				if _, ok := enabled[norm]; !ok {
					t.Fatalf("skill %s invocation %s allow含%q，但 CLI DefaultProfile 未启用", physical.Metadata.Name, invocation.ID, allowed)
				}
			}
		}
	}
	if checked != 4 {
		t.Fatalf("checked %d office/pdf skills, want 4", checked)
	}
}
