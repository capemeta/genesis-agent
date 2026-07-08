package capability

import (
	"context"
	"testing"

	capmodel "genesis-agent/internal/capabilities/capability/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/tool/adapter/registry"
	"genesis-agent/internal/capabilities/tool/gateway"
)

func TestAdapterRegistersToolCapabilityInGateway(t *testing.T) {
	toolRegistry := registry.NewRegistry()
	adapter := New(toolRegistry)
	capability := capmodel.CapabilityIndexRecord{
		ID:          "local@demo:tool:preview",
		Type:        capmodel.CapabilityTypeTool,
		Name:        "preview",
		Description: "Preview tool",
		Spec:        "demo@local",
		Enabled:     true,
		ManifestMetadata: map[string]any{
			"read_only":        true,
			"concurrency_safe": true,
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []any{"path"},
			},
		},
	}
	if err := adapter.Register(context.Background(), capability); err != nil {
		t.Fatal(err)
	}
	gw := gateway.New(toolRegistry, profilemodel.ToolSet{Enabled: []string{"*"}})
	infos := gw.ListInfos()
	if len(infos) != 1 || infos[0].Name != "preview" || infos[0].Parameters == nil || !infos[0].Traits.ReadOnly {
		t.Fatalf("unexpected tool infos: %+v", infos)
	}
}

func TestAdapterDisablesToolCapabilityInGateway(t *testing.T) {
	toolRegistry := registry.NewRegistry()
	adapter := New(toolRegistry)
	capability := capmodel.CapabilityIndexRecord{ID: "local@demo:tool:preview", Type: capmodel.CapabilityTypeTool, Name: "preview", Spec: "demo@local", Enabled: true}
	if err := adapter.Register(context.Background(), capability); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SetEnabled(context.Background(), capability, false); err != nil {
		t.Fatal(err)
	}
	gw := gateway.New(toolRegistry, profilemodel.ToolSet{Enabled: []string{"*"}})
	if got := gw.Get("preview"); got != nil {
		t.Fatal("disabled capability tool should not be executable")
	}
	if len(gw.ListInfos()) != 0 {
		t.Fatalf("disabled capability tool should not be visible: %+v", gw.ListInfos())
	}
}

func TestAdapterUnregisterHidesRegisteredTool(t *testing.T) {
	toolRegistry := registry.NewRegistry()
	adapter := New(toolRegistry)
	capability := capmodel.CapabilityIndexRecord{ID: "local@demo:tool:preview", Type: capmodel.CapabilityTypeTool, Name: "preview", Spec: "demo@local", Enabled: true}
	if err := adapter.Register(context.Background(), capability); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Unregister(context.Background(), capability); err != nil {
		t.Fatal(err)
	}
	gw := gateway.New(toolRegistry, profilemodel.ToolSet{Enabled: []string{"*"}})
	if got := gw.Get("preview"); got != nil {
		t.Fatal("unregistered capability tool should not stay executable in gateway")
	}
	if len(gw.ListInfos()) != 0 {
		t.Fatalf("unregistered capability tool should not be visible: %+v", gw.ListInfos())
	}
}
