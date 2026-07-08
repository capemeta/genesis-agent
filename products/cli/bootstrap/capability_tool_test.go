package bootstrap

import (
	"context"
	"testing"

	capmodel "genesis-agent/internal/capabilities/capability/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/tool/adapter/registry"
	"genesis-agent/internal/capabilities/tool/gateway"
)

type fakeCapabilityRegistry struct {
	records []capmodel.CapabilityIndexRecord
}

func (r fakeCapabilityRegistry) ListCapabilities(context.Context, capmodel.CapabilityQuery) ([]capmodel.CapabilityIndexRecord, error) {
	return append([]capmodel.CapabilityIndexRecord(nil), r.records...), nil
}

func (r fakeCapabilityRegistry) SetCapabilityEnabled(context.Context, string, bool) (capmodel.CapabilityIndexRecord, error) {
	return capmodel.CapabilityIndexRecord{}, nil
}

func TestBuildCapabilityToolsRegistersEnabledToolInGateway(t *testing.T) {
	tools, err := buildCapabilityTools(context.Background(), fakeCapabilityRegistry{records: []capmodel.CapabilityIndexRecord{
		{ID: "local@demo:tool:preview", Type: capmodel.CapabilityTypeTool, Name: "preview", Spec: "demo@local", Enabled: true},
		{ID: "local@demo:tool:hidden", Type: capmodel.CapabilityTypeTool, Name: "hidden", Spec: "demo@local", Enabled: false},
	}})
	if err != nil {
		t.Fatal(err)
	}
	reg := registry.NewRegistry()
	for _, candidate := range tools {
		reg.Register(candidate)
	}
	gw := gateway.New(reg, profilemodel.ToolSet{Enabled: []string{"*"}})
	if got := gw.Get("preview"); got == nil {
		t.Fatal("enabled tool capability should be registered in gateway")
	}
	if got := gw.Get("hidden"); got != nil {
		t.Fatal("disabled tool capability should be hidden from gateway")
	}
}
