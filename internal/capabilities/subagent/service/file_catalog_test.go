package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDefinitionRejectsOrchestrationTools(t *testing.T) {
	definition, err := ParseDefinition("---\nname: api-designer\ndescription: design APIs\ntools: [read_file, Task, write_file]\ndisallowed_tools: [write_file]\nmax_turns: 3\n---\nYou design APIs.")
	if err != nil {
		t.Fatal(err)
	}
	if len(definition.Tools) != 1 || definition.Tools[0] != "read_file" {
		t.Fatalf("unexpected tools: %#v", definition.Tools)
	}
	if definition.MaxTurns != 3 {
		t.Fatalf("unexpected max turns: %d", definition.MaxTurns)
	}
}

func TestParseDefinitionRejectsUnknownFrontmatterField(t *testing.T) {
	_, err := ParseDefinition("---\nname: api-designer\ndescription: design APIs\nunknown_field: value\n---\nYou design APIs.")
	if err == nil {
		t.Fatal("expected unknown frontmatter field to be rejected")
	}
}

func TestParseDefinitionSupportsRuntimeDefaults(t *testing.T) {
	definition, err := ParseDefinition("---\nname: researcher\ndescription: research\nfork_context: true\nexecution_mode: async\ntimeout_seconds: 30\nmax_depth: 2\nmax_tokens: 100\nmax_tool_calls: 4\n---\nResearch safely.")
	if err != nil {
		t.Fatal(err)
	}
	if definition.ForkContext == nil || !*definition.ForkContext || definition.ExecutionMode != "async" || definition.TimeoutSec != 30 || definition.MaxDepth != 2 || definition.MaxTokens != 100 || definition.MaxToolCalls != 4 {
		t.Fatalf("unexpected runtime defaults: %+v", definition)
	}
}

func TestLoadLocalDefinitionsUsesSpecificProjectOverride(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workspace := filepath.Join(root, "workspace", "nested")
	for _, directory := range []string{
		filepath.Join(home, ".genesis", "agents"),
		filepath.Join(root, "workspace", ".genesis", "agents"),
		filepath.Join(workspace, ".genesis", "agents"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeDefinition(t, filepath.Join(home, ".genesis", "agents", "review.md"), "user")
	writeDefinition(t, filepath.Join(root, "workspace", ".genesis", "agents", "review.md"), "parent")
	writeDefinition(t, filepath.Join(workspace, ".genesis", "agents", "review.md"), "workspace")
	definitions, err := LoadLocalDefinitions(workspace, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || definitions[0].Description != "workspace" {
		t.Fatalf("unexpected merged definitions: %+v", definitions)
	}
}

func writeDefinition(t *testing.T, path, description string) {
	t.Helper()
	content := "---\nname: review\ndescription: " + description + "\n---\nReview the code."
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExampleDefinitionIsValid(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "configs", "subagents", "api-designer.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取子智能体配置样例失败: %v", err)
	}
	definition, err := ParseDefinition(string(content))
	if err != nil {
		t.Fatalf("子智能体配置样例必须可加载: %v", err)
	}
	if definition.Name != "api-designer" {
		t.Fatalf("unexpected example definition name: %q", definition.Name)
	}
}
