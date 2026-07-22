package embedded

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	"genesis-agent/internal/capabilities/skill/parser"
)

func TestSourceListReadSearch(t *testing.T) {
	fsys := fstest.MapFS{
		"review/SKILL.md":            {Data: []byte("---\nname: review\ndescription: Review things\n---\nBody")},
		"review/references/guide.md": {Data: []byte("alpha beta")},
		"review/assets/logo.bin":     {Data: []byte{0xff, 0x00, 0x01}},
	}
	source, err := NewSource(model.Authority{Kind: model.SourceKindEmbedded, ID: "test"}, model.ScopeSystem, fsys, parser.New())
	if err != nil {
		t.Fatal(err)
	}
	list, err := source.List(context.Background(), contract.ListQuery{})
	if err != nil || len(list.Packages) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	read, err := source.Read(context.Background(), contract.ReadRequest{PackageID: "review"})
	if err != nil || read.Content != "Body" {
		t.Fatalf("read=%+v err=%v", read, err)
	}
	search, err := source.Search(context.Background(), contract.SearchRequest{PackageID: "review", Query: "alpha"})
	if err != nil || len(search.Matches) != 1 {
		t.Fatalf("search=%+v err=%v", search, err)
	}
	byName, err := source.Search(context.Background(), contract.SearchRequest{PackageID: "review", Query: "guide.md"})
	if err != nil || len(byName.Matches) != 1 {
		t.Fatalf("filename search=%+v err=%v", byName, err)
	}
	resources, err := source.ListResources(context.Background(), contract.SourceListResourcesRequest{PackageID: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Resources) != 2 {
		t.Fatalf("resources=%+v", resources)
	}
	if resources.Resources[1].Resource != "review/references/guide.md" || !resources.Resources[1].Text {
		t.Fatalf("text resource=%+v", resources.Resources[1])
	}
	if resources.Resources[0].Resource != "review/assets/logo.bin" || resources.Resources[0].Text {
		t.Fatalf("binary resource=%+v", resources.Resources[0])
	}
}

func TestSystemFSIncludesSkillCreator(t *testing.T) {
	fsys, err := SystemFS()
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(model.Authority{Kind: model.SourceKindEmbedded, ID: "system-test"}, model.ScopeSystem, fsys, parser.New())
	if err != nil {
		t.Fatal(err)
	}
	listed, err := source.List(context.Background(), contract.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range listed.Packages {
		if entry.Metadata.Name == "skill-creator" {
			return
		}
	}
	t.Fatalf("skill-creator not found in system skills: %+v", listed)
}

func TestSystemFSIncludesOfficeSkills(t *testing.T) {
	fsys, err := SystemFS()
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(model.Authority{Kind: model.SourceKindEmbedded, ID: "system-test"}, model.ScopeSystem, fsys, parser.New())
	if err != nil {
		t.Fatal(err)
	}
	listed, err := source.List(context.Background(), contract.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"office-word":  false,
		"office-excel": false,
		"office-ppt":   false,
		"office-pdf":   false,
	}
	for _, entry := range listed.Packages {
		if strings.HasPrefix(entry.Metadata.Name, "_") {
			t.Fatalf("shared package should not appear in catalog: %s", entry.Metadata.Name)
		}
		if _, ok := want[entry.Metadata.Name]; ok {
			want[entry.Metadata.Name] = true
			if entry.Manifest == nil || len(entry.Manifest.Invocations) == 0 || len(entry.Manifest.RuntimeProfiles) == 0 {
				t.Fatalf("%s missing runtime sidecar", entry.Metadata.Name)
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("%s not found in system skills: %+v", name, listed)
		}
	}

	resources, err := source.ListResources(context.Background(), contract.SourceListResourcesRequest{PackageID: "office-word"})
	if err != nil {
		t.Fatal(err)
	}
	var hasComment, hasValidate bool
	for _, resource := range resources.Resources {
		if resource.Resource == "office-word/scripts/comment.py" && resource.Text {
			hasComment = true
		}
		if resource.Resource == "office-word/scripts/office/validate.py" && resource.Text {
			hasValidate = true
		}
	}
	if !hasComment || !hasValidate {
		t.Fatalf("office-word missing migrated scripts: comment=%v validate=%v count=%d resources=%+v", hasComment, hasValidate, len(resources.Resources), resources.Resources)
	}

	pptResources, err := source.ListResources(context.Background(), contract.SourceListResourcesRequest{PackageID: "office-ppt"})
	if err != nil {
		t.Fatal(err)
	}
	var hasThumb, hasUnpack bool
	for _, resource := range pptResources.Resources {
		if resource.Resource == "office-ppt/scripts/thumbnail.py" {
			hasThumb = true
		}
		if resource.Resource == "office-ppt/scripts/office/unpack.py" {
			hasUnpack = true
		}
	}
	if !hasThumb || !hasUnpack {
		t.Fatalf("office-ppt missing migrated scripts: thumb=%v unpack=%v count=%d", hasThumb, hasUnpack, len(pptResources.Resources))
	}

	design, err := source.Read(context.Background(), contract.ReadRequest{PackageID: "office-ppt", Resource: "office-ppt/design.md"})
	if err != nil || !strings.Contains(design.Content, "Design Ideas") {
		t.Fatalf("design.md read=%+v err=%v", design, err)
	}

	pdfResources, err := source.ListResources(context.Background(), contract.SourceListResourcesRequest{PackageID: "office-pdf"})
	if err != nil {
		t.Fatal(err)
	}
	var hasFill, hasForms, hasCJK bool
	for _, resource := range pdfResources.Resources {
		if resource.Resource == "office-pdf/scripts/fill_fillable_fields.py" && resource.Text {
			hasFill = true
		}
		if resource.Resource == "office-pdf/FORMS.md" && resource.Text {
			hasForms = true
		}
		if resource.Resource == "office-pdf/scripts/register_cjk_font.py" && resource.Text {
			hasCJK = true
		}
	}
	if !hasFill || !hasForms || !hasCJK {
		t.Fatalf("office-pdf missing migrated assets: fill=%v forms=%v cjk=%v count=%d", hasFill, hasForms, hasCJK, len(pdfResources.Resources))
	}

	excelResources, err := source.ListResources(context.Background(), contract.SourceListResourcesRequest{PackageID: "office-excel"})
	if err != nil {
		t.Fatal(err)
	}
	var hasRecalc, hasSoffice bool
	for _, resource := range excelResources.Resources {
		if resource.Resource == "office-excel/scripts/recalc.py" && resource.Text {
			hasRecalc = true
		}
		if resource.Resource == "office-excel/scripts/office/soffice.py" && resource.Text {
			hasSoffice = true
		}
		if strings.Contains(string(resource.Resource), "inspect_xlsx") || strings.Contains(string(resource.Resource), "recalc_xlsx") || strings.Contains(string(resource.Resource), "path_contract") || strings.Contains(string(resource.Resource), "validation-checklist") {
			t.Fatalf("office-excel still has forbidden legacy resource: %s", resource.Resource)
		}
	}
	if !hasRecalc || !hasSoffice {
		t.Fatalf("office-excel missing migrated scripts: recalc=%v soffice=%v count=%d", hasRecalc, hasSoffice, len(excelResources.Resources))
	}
}

func TestPortableOfficeSkillBodiesDoNotReferenceHostProtocol(t *testing.T) {
	fsys, err := SystemFS()
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(model.Authority{Kind: model.SourceKindEmbedded, ID: "system-test"}, model.ScopeSystem, fsys, parser.New())
	if err != nil {
		t.Fatal(err)
	}

	// allowed-tools 等宿主装配元数据属于 frontmatter；可移植的任务知识正文不得依赖 Genesis 工具协议。
	forbidden := []string{
		"run_skill_command",
		"`write_file`",
		"publish_artifact",
		"$WORK_DIR",
		"Skill Harness",
		"file-editing tool",
	}
	for _, packageID := range []string{"office-word", "office-excel", "office-ppt", "office-pdf"} {
		read, readErr := source.Read(context.Background(), contract.ReadRequest{PackageID: model.PackageID(packageID)})
		if readErr != nil {
			t.Fatalf("read %s: %v", packageID, readErr)
		}
		for _, token := range forbidden {
			if strings.Contains(read.Content, token) {
				t.Errorf("%s body references host protocol %q", packageID, token)
			}
		}
	}
}

func TestOfficePPTShortResourceRejectedAtSourceWithoutQualify(t *testing.T) {
	fsys, err := SystemFS()
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(model.Authority{Kind: model.SourceKindEmbedded, ID: "system-test"}, model.ScopeSystem, fsys, parser.New())
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Read(context.Background(), contract.ReadRequest{PackageID: "office-ppt", Resource: "design.md"})
	if err == nil {
		t.Fatal("bare design.md should fail at source; service layer must Qualify first")
	}
}
