package gateway

import (
	"context"
	"fmt"
	"testing"

	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

type fakeRegistry struct {
	tools map[string]tool.Tool
}

type hookDispatcher struct {
	events []hookmodel.Event
	result hookmodel.AggregateResult
}

func (d *hookDispatcher) Dispatch(_ context.Context, event hookmodel.Event) (hookmodel.AggregateResult, error) {
	d.events = append(d.events, event)
	return d.result, nil
}

func TestGatewayDispatchesToolHooksAndAppliesInputUpdate(t *testing.T) {
	reg := &fakeRegistry{tools: map[string]tool.Tool{}}
	called := ""
	reg.Register(funcTool{name: "calculator", execute: func(_ context.Context, params string) (string, error) { called = params; return "ok", nil }})
	dispatcher := &hookDispatcher{result: hookmodel.AggregateResult{UpdatedInput: map[string]any{"expression": "2+2"}, AdditionalContext: []string{"hook context"}}}
	ctx := hookcontract.WithDispatcher(context.Background(), dispatcher)
	g := New(reg, profilemodel.ToolSet{Enabled: []string{"calculator"}})
	if _, err := g.Execute(ctx, "calculator", `{}`); err != nil {
		t.Fatal(err)
	}
	if called != `{"expression":"2+2"}` || len(dispatcher.events) != 2 {
		t.Fatalf("called=%q events=%#v", called, dispatcher.events)
	}
	if additions := hookcontract.DrainAdditionalContext(ctx); len(additions) != 2 {
		t.Fatalf("additions=%#v", additions)
	}
}

type funcTool struct {
	name    string
	execute func(context.Context, string) (string, error)
}

func (t funcTool) GetInfo() *tool.Info { return &tool.Info{Name: t.name} }
func (t funcTool) Execute(ctx context.Context, params string) (string, error) {
	return t.execute(ctx, params)
}

func newFakeRegistry() *fakeRegistry {
	r := &fakeRegistry{tools: map[string]tool.Tool{}}
	r.Register(fakeTool{name: "current_time"})
	r.Register(fakeTool{name: "calculator"})
	r.Register(fakeTool{name: "http_request"})
	return r
}

func (r *fakeRegistry) Register(t tool.Tool) error {
	if _, exists := r.tools[t.GetInfo().Name]; exists {
		return fmt.Errorf("duplicate tool: %s", t.GetInfo().Name)
	}
	r.tools[t.GetInfo().Name] = t
	return nil
}
func (r *fakeRegistry) Replace(name, expectedOwner string, t tool.Tool) error {
	if _, exists := r.tools[name]; !exists || expectedOwner != "fake" {
		return fmt.Errorf("replace rejected")
	}
	r.tools[name] = t
	return nil
}
func (r *fakeRegistry) Owner(name string) (string, bool) {
	_, ok := r.tools[name]
	return "fake", ok
}
func (r *fakeRegistry) Unregister(name string)    { delete(r.tools, name) }
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

func TestGatewayExposesSkillTool(t *testing.T) {
	reg := &fakeRegistry{tools: map[string]tool.Tool{}}
	reg.Register(fakeTool{name: "Skill"})
	g := New(reg, profilemodel.ToolSet{Enabled: []string{"Skill"}})
	if got := g.Get("Skill"); got == nil || got.GetInfo().Name != "Skill" {
		t.Fatalf("Get(Skill) = %#v", got)
	}
	if got := g.Get("load_skill"); got != nil {
		t.Fatalf("Get(load_skill) should be nil after alias removal, got %#v", got)
	}
	infos := g.ListInfos()
	if len(infos) != 1 || infos[0].Name != "Skill" {
		t.Fatalf("ListInfos = %#v", infos)
	}
}

func TestGatewayIsRegisteredIgnoresProfile(t *testing.T) {
	reg := newFakeRegistry()
	g := New(reg, profilemodel.ToolSet{Enabled: []string{"calculator"}})
	if !g.IsRegistered("http_request") {
		t.Fatal("http_request should be registered even when profile-disabled")
	}
	if g.Get("http_request") != nil {
		t.Fatal("Get should still respect profile")
	}
}

func TestGatewaySnapshotUsesDescriptionFunc(t *testing.T) {
	reg := &fakeRegistry{tools: map[string]tool.Tool{}}
	reg.Register(dynamicTool{name: "Skill", staticDesc: "static", dynamicDesc: "dynamic-catalog"})
	g := New(reg, profilemodel.ToolSet{Enabled: []string{"Skill"}})
	infos := g.ListInfosContext(context.Background())
	if len(infos) != 1 || infos[0].Description != "dynamic-catalog" {
		t.Fatalf("ListInfosContext = %#v", infos)
	}
	if infos[0].DescriptionFunc != nil {
		t.Fatal("SnapshotForLLM should clear DescriptionFunc")
	}
}

type dynamicTool struct {
	name        string
	staticDesc  string
	dynamicDesc string
}

func (t dynamicTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        t.name,
		Description: t.staticDesc,
		DescriptionFunc: func(context.Context) (string, error) {
			return t.dynamicDesc, nil
		},
		Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect},
	}
}

func (t dynamicTool) Execute(context.Context, string) (string, error) {
	return "ok", nil
}
