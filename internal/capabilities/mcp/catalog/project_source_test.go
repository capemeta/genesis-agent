package catalog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"genesis-agent/internal/capabilities/mcp/catalog"
	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
)

func TestProjectSourceLoadsYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	genesis := filepath.Join(dir, ".genesis")
	if err := os.MkdirAll(genesis, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte("servers:\n  demo:\n    type: stdio\n    command: echo\n    enabled: true\n")
	if err := os.WriteFile(filepath.Join(genesis, "mcp.yaml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	src := catalog.NewProjectSource(dir)
	defs, err := src.List(context.Background(), contract.RuntimeCatalogEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("got %d defs", len(defs))
	}
	if defs[0].Config.Name != "demo" || defs[0].Origin != model.OriginProject {
		t.Fatalf("unexpected def: %+v", defs[0])
	}
	if src.Precedence() != 40 {
		t.Fatalf("precedence=%d", src.Precedence())
	}
}
