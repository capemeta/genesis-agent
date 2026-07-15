package catalog

import (
	"context"
	"testing"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
)

type staticSource struct {
	precedence int
	defs       []model.McpServerDefinition
}

func (s staticSource) Precedence() int { return s.precedence }
func (s staticSource) List(context.Context, contract.RuntimeCatalogEnv) ([]model.McpServerDefinition, error) {
	return s.defs, nil
}

func TestCatalogMergePrecedence(t *testing.T) {
	cat := New([]contract.DefinitionSource{
		staticSource{precedence: 10, defs: []model.McpServerDefinition{{
			Config: model.McpServerConfig{Name: "fs", Command: "a", Enabled: true},
			Origin: model.OriginConfig,
		}}},
		staticSource{precedence: 50, defs: []model.McpServerDefinition{{
			Config: model.McpServerConfig{Name: "fs", Command: "b", Enabled: true},
			Origin: model.OriginMarketplace,
		}}},
	}, nil)
	defs, err := cat.Merge(context.Background(), contract.RuntimeCatalogEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("len=%d", len(defs))
	}
	if defs[0].Config.Command != "b" {
		t.Fatalf("winner command=%q", defs[0].Config.Command)
	}
	if len(defs[0].OverriddenOrigins) != 1 || defs[0].OverriddenOrigins[0] != model.OriginConfig {
		t.Fatalf("overridden=%v", defs[0].OverriddenOrigins)
	}
}
