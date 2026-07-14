package service

import (
	"context"
	"testing"

	"genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
)

func TestOfficePPTUsesVerbatimAnthropicLayout(t *testing.T) {
	fsys, err := embedded.SystemFS()
	if err != nil {
		t.Fatal(err)
	}
	source, err := embedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "system-test"}, skillmodel.ScopeSystem, fsys, skillparser.New())
	if err != nil {
		t.Fatal(err)
	}
	resources, err := source.ListResources(context.Background(), skillcontract.SourceListResourcesRequest{PackageID: "office-ppt"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, resource := range resources.Resources {
		seen[string(resource.Resource)] = true
	}
	for _, want := range []string{"office-ppt/editing.md", "office-ppt/pptxgenjs.md", "office-ppt/LICENSE.txt", "office-ppt/scripts/thumbnail.py", "office-ppt/scripts/office/unpack.py"} {
		if !seen[want] {
			t.Fatalf("missing %s", want)
		}
	}
	for _, forbidden := range []string{"office-ppt/references/editing.md", "office-ppt/scripts/path_contract.py", "office-ppt/scripts/create_pptx.js", "office-ppt/scripts/run_pptxgen_script.js", "office-ppt/scripts/inspect_pptx.py", "office-ppt/scripts/extract_pptx_text.py"} {
		if seen[forbidden] {
			t.Fatalf("forbidden legacy resource still exists: %s", forbidden)
		}
	}
}

func TestOfficeExcelUsesVerbatimAnthropicLayout(t *testing.T) {
	fsys, err := embedded.SystemFS()
	if err != nil {
		t.Fatal(err)
	}
	source, err := embedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "system-test"}, skillmodel.ScopeSystem, fsys, skillparser.New())
	if err != nil {
		t.Fatal(err)
	}
	resources, err := source.ListResources(context.Background(), skillcontract.SourceListResourcesRequest{PackageID: "office-excel"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, resource := range resources.Resources {
		seen[string(resource.Resource)] = true
	}
	for _, want := range []string{"office-excel/LICENSE.txt", "office-excel/scripts/recalc.py", "office-excel/scripts/office/soffice.py", "office-excel/scripts/office/unpack.py"} {
		if !seen[want] {
			t.Fatalf("missing %s", want)
		}
	}
	for _, forbidden := range []string{
		"office-excel/scripts/path_contract.py",
		"office-excel/scripts/inspect_xlsx.py",
		"office-excel/scripts/recalc_xlsx.py",
		"office-excel/references/validation-checklist.md",
	} {
		if seen[forbidden] {
			t.Fatalf("forbidden legacy resource still exists: %s", forbidden)
		}
	}
}
