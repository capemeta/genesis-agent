package parser

import (
	"strings"
	"testing"

	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

func TestParseFullSkillOnlyAcceptsPortableFrontmatter(t *testing.T) {
	data := []byte("---\nname: code-review\ndescription: Review code carefully\n---\nBody")
	meta, body, err := New().ParseFull(data, contract.ParseSource{Authority: model.Authority{Kind: model.SourceKindHost, ID: "test"}, Scope: model.ScopeProject, DirectoryName: "code-review"})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "code-review" || meta.Description != "Review code carefully" || body != "Body" {
		t.Fatalf("meta=%+v body=%q", meta, body)
	}
}

func TestParseRejectsRuntimeFieldsAndDirectoryMismatch(t *testing.T) {
	for _, data := range []string{
		"---\nname: code-review\ndescription: desc\ncontext: fork\n---\nBody",
		"---\nname: other\ndescription: desc\n---\nBody",
	} {
		_, _, err := New().ParseFull([]byte(data), contract.ParseSource{DirectoryName: "code-review"})
		if err == nil {
			t.Fatalf("expected rejection for %q", data)
		}
	}
}

func TestParseRuntimeManifestStrictMultiInvocation(t *testing.T) {
	manifest, err := New().ParseRuntimeManifest([]byte(`schema: genesis.skill/v1
skill: demo
runtime_profiles:
  read:
    sandbox: {required: true, execution_mode: per_call}
invocations:
  - id: read
    handle: demo-read
    description: Read demo documents
    agent_mode: main
    runtime_profile: read
    request:
      task: {required: false}
      inputs: {min_items: 1, max_items: 1, access: read_only, accepted_suffixes: [.pptx]}
    prompt: {skill_body: omit}
    tool_policy: {allow: [run_skill_command], required: [run_skill_command]}
    result: {kind: message}
`), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Invocations) != 1 || manifest.Invocations[0].Handle != "demo-read" {
		t.Fatalf("manifest=%+v", manifest)
	}
}

func TestParseRuntimeManifestRejectsUnknownAndDuplicateFields(t *testing.T) {
	base := `schema: genesis.skill/v1
skill: demo
runtime_profiles:
  read:
    sandbox: {required: true, execution_mode: per_call}
invocations: []
`
	for _, data := range []string{base + "unknown: true\n", strings.Replace(base, "skill: demo", "skill: demo\nskill: demo", 1)} {
		if _, err := New().ParseRuntimeManifest([]byte(data), "demo"); err == nil {
			t.Fatalf("expected strict YAML rejection: %s", data)
		}
	}
}
