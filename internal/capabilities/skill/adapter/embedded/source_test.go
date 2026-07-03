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
}
