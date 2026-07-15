package service

import "testing"

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
