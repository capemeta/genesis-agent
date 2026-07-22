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
	if len(listed.Packages) != 1 || listed.Packages[0].Metadata.Name != "review" || listed.Packages[0].Snapshot.Digest == "" {
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
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review\ndescription: Review carefully\n---\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "guide.md"), []byte("alpha beta gamma"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "editing.md"), []byte("root guide"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `schema: genesis.skill/v1
skill: review
runtime_profiles:
  read:
    sandbox: {required: true, execution_mode: per_call}
invocations:
  - id: read
    handle: review
    description: Read review material
    agent_mode: main
    runtime_profile: read
    request: {task: {required: false}, inputs: {min_items: 0, max_items: 1, access: read_only}}
    prompt: {skill_body: include}
    tool_policy: {allow: [run_skill_command], required: [run_skill_command]}
    result: {kind: message}
`
	if err := os.WriteFile(filepath.Join(dir, model.RuntimeManifestFileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "logo.bin"), []byte{0xff, 0x00, 0x01}, 0o644); err != nil {
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
	resources, err := source.ListResources(context.Background(), contract.SourceListResourcesRequest{PackageID: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Resources) != 3 {
		t.Fatalf("resources = %+v", resources)
	}
	if resources.Resources[0].Resource != "review/assets/logo.bin" || resources.Resources[0].Text {
		t.Fatalf("binary resource = %+v", resources.Resources[0])
	}
	if resources.Resources[1].Resource != "review/editing.md" || !resources.Resources[1].Text || resources.Resources[2].Resource != "review/references/guide.md" {
		t.Fatalf("text resources = %+v", resources.Resources)
	}
	if _, err := source.Read(context.Background(), contract.ReadRequest{PackageID: "review", Resource: "review/editing.md"}); err != nil {
		t.Fatalf("root markdown must be readable: %v", err)
	}
	if _, err := source.Read(context.Background(), contract.ReadRequest{PackageID: "review", Resource: "review/" + model.RuntimeManifestFileName}); err == nil {
		t.Fatal("runtime sidecar must not be model-readable")
	}
}
