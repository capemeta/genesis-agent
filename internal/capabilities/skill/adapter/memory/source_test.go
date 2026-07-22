package memory

import (
	"context"
	"testing"

	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

func TestSourceListReadSearch(t *testing.T) {
	meta := model.Metadata{Name: "review", Description: "Review", Authority: model.Authority{Kind: model.SourceKindEmbedded, ID: "test"}, PackageID: "review", MainResource: "review/SKILL.md"}.Normalize()
	source := NewSource(meta.Authority, []Skill{{Metadata: meta, Body: "Body", Resources: map[model.ResourceID]string{"review/references/guide.md": "alpha beta"}}})
	list, err := source.List(context.Background(), contract.ListQuery{})
	if err != nil || len(list.Packages) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if list.Packages[0].Snapshot.Digest == "" || list.Packages[0].Metadata.Name != "review" {
		t.Fatalf("package = %+v", list.Packages[0])
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
}
