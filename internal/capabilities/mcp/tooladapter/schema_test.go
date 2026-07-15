package tooladapter

import (
	"encoding/json"
	"testing"
)

func TestConvertInputSchema(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"p"}},"required":["path"]}`)
	schema := ConvertInputSchema(raw)
	if schema.Type != "object" {
		t.Fatalf("type=%s", schema.Type)
	}
	if schema.Properties["path"] == nil || schema.Properties["path"].Type != "string" {
		t.Fatalf("properties=%v", schema.Properties)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "path" {
		t.Fatalf("required=%v", schema.Required)
	}
}
