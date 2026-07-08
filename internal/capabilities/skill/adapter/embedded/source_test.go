package embedded

import (
	"context"
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
	if err != nil || len(list.Entries) != 1 {
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
	for _, entry := range listed.Entries {
		if entry.Name == "skill-creator" {
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
		"pdf-review":   false,
	}
	for _, entry := range listed.Entries {
		if _, ok := want[entry.Name]; ok {
			want[entry.Name] = true
			if len(entry.AllowedTools) == 0 {
				t.Fatalf("%s missing allowed tools", entry.Name)
			}
			if len(entry.Dependencies.Tools) == 0 {
				t.Fatalf("%s missing dependencies", entry.Name)
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
	var hasScript, hasReference bool
	for _, resource := range resources.Resources {
		if resource.Resource == "office-word/scripts/inspect_docx.py" && resource.Text {
			hasScript = true
		}
		if resource.Resource == "office-word/references/validation-checklist.md" && resource.Text {
			hasReference = true
		}
	}
	if !hasScript || !hasReference {
		t.Fatalf("office-word resources=%+v", resources.Resources)
	}
}
