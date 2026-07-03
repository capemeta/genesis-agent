package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	"genesis-agent/internal/capabilities/skill/parser"
)

func TestLocalSourceListsAndReadsSkill(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review\ndescription: Review carefully\n---\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(model.Authority{Kind: model.SourceKindHost, ID: "local-test"}, []Root{{Path: root, Scope: model.ScopeProject}}, parser.New())
	if err != nil {
		t.Fatal(err)
	}
	listed, err := source.List(context.Background(), contract.ListQuery{Product: profilemodel.ChannelCLI})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Entries) != 1 || listed.Entries[0].Name != "review" {
		t.Fatalf("listed = %+v", listed)
	}
	read, err := source.Read(context.Background(), contract.ReadRequest{PackageID: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if read.Metadata.Name != "review" || read.Content != "Body" {
		t.Fatalf("read = %+v", read)
	}
}

func TestLocalSourceReadsAndSearchesPackageResources(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "review")
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review\ndescription: Review carefully\n---\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "guide.md"), []byte("alpha beta gamma"), 0o644); err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(model.Authority{Kind: model.SourceKindHost, ID: "local-test"}, []Root{{Path: root, Scope: model.ScopeProject}}, parser.New())
	if err != nil {
		t.Fatal(err)
	}
	read, err := source.Read(context.Background(), contract.ReadRequest{PackageID: "review", Resource: "review/references/guide.md"})
	if err != nil {
		t.Fatal(err)
	}
	if read.Content != "alpha beta gamma" {
		t.Fatalf("content = %q", read.Content)
	}
	searched, err := source.Search(context.Background(), contract.SearchRequest{PackageID: "review", Query: "beta", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(searched.Matches) != 1 || searched.Matches[0].Resource != "review/references/guide.md" {
		t.Fatalf("searched = %+v", searched)
	}
}
