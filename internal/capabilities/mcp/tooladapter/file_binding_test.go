package tooladapter

import (
	"encoding/json"
	"testing"

	"genesis-agent/internal/capabilities/mcp/model"
)

func TestDiscoverFileBindingsUsesSchemaSemanticsNotFieldNames(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"source":{"type":"string","format":"file-path"},"result":{"type":"string","x-genesis-kind":"artifact"}}}`)
	bindings := DiscoverFileBindings(schema)
	if len(bindings) != 2 || bindings[0].JSONPointer != "/result" || bindings[0].Kind != model.MCPFileBindingArtifact || bindings[1].JSONPointer != "/source" {
		t.Fatalf("bindings = %+v", bindings)
	}
}
