package parser

import (
	"testing"

	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

func TestParseFullSkill(t *testing.T) {
	data := []byte("---\nname: code-review\ndescription: Review code carefully\nallowed-tools:\n  - read_file\ncontext: inline\n---\nBody")
	meta, body, err := New().ParseFull(data, contract.ParseSource{Authority: model.Authority{Kind: model.SourceKindHost, ID: "test"}, Scope: model.ScopeProject, DirectoryName: "code-review"})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "code-review" || meta.Description == "" || len(meta.AllowedTools) != 1 {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	if body != "Body" {
		t.Fatalf("body = %q", body)
	}
}

func TestParseRejectsDirectoryNameMismatch(t *testing.T) {
	data := []byte("---\nname: other\ndescription: desc\n---\nBody")
	_, _, err := New().ParseFull(data, contract.ParseSource{DirectoryName: "code-review"})
	if err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestParseDependencies(t *testing.T) {
	data := []byte("---\nname: code-review\ndescription: Review code carefully\ndependencies:\n  tools:\n    - read_file\n    - type: mcp\n      value: github\n      transport: stdio\n---\nBody")
	meta, _, err := New().ParseFull(data, contract.ParseSource{Authority: model.Authority{Kind: model.SourceKindHost, ID: "test"}, Scope: model.ScopeProject, DirectoryName: "code-review"})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Dependencies.Tools) != 2 {
		t.Fatalf("dependencies = %+v", meta.Dependencies)
	}
	if meta.Dependencies.Tools[1].Type != "mcp" || meta.Dependencies.Tools[1].Transport != "stdio" {
		t.Fatalf("unexpected dependency: %+v", meta.Dependencies.Tools[1])
	}
}

func TestParseDependenciesRuntime(t *testing.T) {
	data := []byte(`---
name: office-ppt
description: PPT skill
dependencies:
  tools:
    - type: tool
      value: run_skill_command
  runtime:
    node:
      - name: pptxgenjs
        require: pptxgenjs
    python:
      - name: pillow
        import: PIL
  install_hints:
    - npm install pptxgenjs
---
Body`)
	meta, _, err := New().ParseFull(data, contract.ParseSource{Authority: model.Authority{Kind: model.SourceKindHost, ID: "test"}, Scope: model.ScopeProject, DirectoryName: "office-ppt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Dependencies.Runtime.Node) != 1 || meta.Dependencies.Runtime.Node[0].Name != "pptxgenjs" || meta.Dependencies.Runtime.Node[0].Require != "pptxgenjs" {
		t.Fatalf("node runtime = %+v", meta.Dependencies.Runtime.Node)
	}
	if len(meta.Dependencies.Runtime.Python) != 1 || meta.Dependencies.Runtime.Python[0].Name != "pillow" {
		t.Fatalf("python runtime = %+v", meta.Dependencies.Runtime.Python)
	}
	if len(meta.Dependencies.InstallHints) != 1 {
		t.Fatalf("install_hints = %+v", meta.Dependencies.InstallHints)
	}
	wl := meta.Dependencies.RuntimeWhitelist()
	if _, ok := wl["npm:pptxgenjs"]; !ok {
		t.Fatalf("whitelist missing npm:pptxgenjs: %+v", wl)
	}
	if _, ok := wl["pip:pillow"]; !ok {
		t.Fatalf("whitelist missing pip:pillow: %+v", wl)
	}
}
