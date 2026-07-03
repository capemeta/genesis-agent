package gateway

import (
	"context"
	"testing"

	profilemodel "genesis-agent/internal/capabilities/profile/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

type fakeRegistry struct {
	tools map[string]tool.Tool
}

func newFakeRegistry() *fakeRegistry {
	r := &fakeRegistry{tools: map[string]tool.Tool{}}
	r.Register(fakeTool{name: "current_time"})
	r.Register(fakeTool{name: "calculator"})
	r.Register(fakeTool{name: "http_request"})
	return r
}

func (r *fakeRegistry) Register(t tool.Tool)      { r.tools[t.GetInfo().Name] = t }
func (r *fakeRegistry) Get(name string) tool.Tool { return r.tools[name] }
func (r *fakeRegistry) Execute(ctx context.Context, name, params string) (string, error) {
	return r.tools[name].Execute(ctx, params)
}
func (r *fakeRegistry) ListInfos() []*tool.Info {
	infos := make([]*tool.Info, 0, len(r.tools))
	for _, t := range r.tools {
		infos = append(infos, t.GetInfo())
	}
	return infos
}
func (r *fakeRegistry) FilterInfos(names []string) []*tool.Info {
	infos := make([]*tool.Info, 0, len(names))
	for _, name := range names {
		if t := r.Get(name); t != nil {
			infos = append(infos, t.GetInfo())
		}
	}
	return infos
}
func (r *fakeRegistry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

type fakeTool struct {
	name   string
	traits tool.ToolTraits
}

func (t fakeTool) GetInfo() *tool.Info { return &tool.Info{Name: t.name, Traits: t.traits} }
func (t fakeTool) Execute(context.Context, string) (string, error) {
	return "ok:" + t.name, nil
}

func TestGatewayAllowsConfiguredBuiltinTools(t *testing.T) {
	g := New(newFakeRegistry(), profilemodel.ToolSet{Enabled: []string{"current_time", "calculator", "http_request"}})
	infos := g.ListInfos()
	if len(infos) != 3 {
		t.Fatalf("ListInfos() length = %d, want 3", len(infos))
	}
	got, err := g.Execute(context.Background(), "http_request", `{}`)
	if err != nil {
		t.Fatalf("Execute(http_request) error = %v", err)
	}
	if got != "ok:http_request" {
		t.Fatalf("Execute(http_request) = %q, want ok:http_request", got)
	}
}

func TestGatewayDeniedToolIsNotVisibleOrExecutable(t *testing.T) {
	g := New(newFakeRegistry(), profilemodel.ToolSet{Enabled: []string{"*"}, Disabled: []string{"http_request"}})
	if got := g.Get("http_request"); got != nil {
		t.Fatal("Get(http_request) returned tool, want nil")
	}
	if _, err := g.Execute(context.Background(), "http_request", `{}`); err == nil {
		t.Fatal("Execute(http_request) error = nil, want denied error")
	}
	for _, info := range g.ListInfos() {
		if info.Name == "http_request" {
			t.Fatal("ListInfos() contains disabled http_request")
		}
	}
}

func TestGatewayFilterInfosAppliesPolicy(t *testing.T) {
	g := New(newFakeRegistry(), profilemodel.ToolSet{Enabled: []string{"calculator"}})
	infos := g.FilterInfos([]string{"calculator", "http_request"})
	if len(infos) != 1 || infos[0].Name != "calculator" {
		t.Fatalf("FilterInfos() = %#v, want only calculator", infos)
	}
}
