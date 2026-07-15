package gateway

import (
	"context"
	"sync"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
	mcpscope "genesis-agent/internal/capabilities/mcp/scope"
	"genesis-agent/internal/capabilities/mcp/tooladapter"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

const deferredToolThreshold = 8

// Options MCP 域网关协调器选项。
type Options struct {
	Manager  contract.Manager
	Registry tool.Registry
	ScopeEnv contract.RuntimeCatalogEnv
	OnEvent  func(ctx context.Context, event model.LifecycleEvent)
}

// Gateway 订阅 Manager 状态，把存活 server 的 tool 投影并 Register/Unregister 到 Tool Registry。
// 不做连接（连接由 Manager 负责）。
type Gateway struct {
	manager  contract.Manager
	registry tool.Registry
	scopeEnv contract.RuntimeCatalogEnv
	onEvent  func(ctx context.Context, event model.LifecycleEvent)

	mu       sync.Mutex
	byServer map[string][]string // server -> model tool names
	defs     map[string]model.McpServerDefinition
}

// New 创建 MCP gateway 协调器并订阅 Manager。
func New(opts Options) *Gateway {
	g := &Gateway{
		manager:  opts.Manager,
		registry: opts.Registry,
		scopeEnv: opts.ScopeEnv,
		onEvent:  opts.OnEvent,
		byServer: make(map[string][]string),
		defs:     make(map[string]model.McpServerDefinition),
	}
	if g.manager != nil {
		g.manager.Subscribe(g)
	}
	return g
}

// SetDefinitions 更新 server 定义（用于暴露策略 / 超时）。
func (g *Gateway) SetDefinitions(defs []model.McpServerDefinition) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.defs = make(map[string]model.McpServerDefinition, len(defs))
	for _, d := range defs {
		g.defs[d.Config.Name] = d
	}
}

// OnMCPEvent 实现 contract.StateListener。
func (g *Gateway) OnMCPEvent(ctx context.Context, event model.LifecycleEvent) {
	if g.onEvent != nil {
		g.onEvent(ctx, event)
	}
	switch event.Kind {
	case model.EventServerReady, model.EventToolsChanged:
		g.projectServer(ctx, event.State)
	case model.EventServerFailed, model.EventServerClosed:
		g.unprojectServer(event.Server)
	}
}

// SyncFromStates 根据当前 Manager 状态全量投影（启动后调用一次）。
func (g *Gateway) SyncFromStates(ctx context.Context) {
	if g.manager == nil {
		return
	}
	for _, st := range g.manager.States() {
		if st.Status == model.ServerStatusReady {
			g.projectServer(ctx, st)
		}
	}
}

func (g *Gateway) projectServer(ctx context.Context, state model.ServerState) {
	_ = ctx
	if g.registry == nil || g.manager == nil || state.Name == "" {
		return
	}
	g.mu.Lock()
	def, ok := g.defs[state.Name]
	g.mu.Unlock()
	if ok && !mcpscope.Allows(def.Config.Scope, g.scopeEnv) {
		g.unprojectServer(state.Name)
		return
	}

	exposure := tool.ToolExposureDirect
	timeout := def.Config.ToolTimeout
	if ok && def.Config.Exposure != "" {
		exposure = def.Config.Exposure
	}
	if exposure == tool.ToolExposureDirect && len(state.Tools) > deferredToolThreshold {
		exposure = tool.ToolExposureDeferred
	}

	deduper := NewDeduper()
	// 保留其他 server 已占用名，避免跨 server 冲突。
	g.mu.Lock()
	for srv, names := range g.byServer {
		if srv == state.Name {
			continue
		}
		for _, n := range names {
			deduper.used[n] = struct{}{}
		}
	}
	oldNames := g.byServer[state.Name]
	g.mu.Unlock()

	for _, name := range oldNames {
		g.registry.Unregister(name)
	}

	newNames := make([]string, 0, len(state.Tools))
	for _, snap := range state.Tools {
		modelName := deduper.Unique(state.Name, snap.Name)
		t := tooladapter.New(g.manager, state.Name, snap.Name, modelName, snap, exposure, timeout)
		g.registry.Register(t)
		newNames = append(newNames, modelName)
	}
	g.mu.Lock()
	g.byServer[state.Name] = newNames
	g.mu.Unlock()
}

func (g *Gateway) unprojectServer(server string) {
	g.mu.Lock()
	names := g.byServer[server]
	delete(g.byServer, server)
	g.mu.Unlock()
	if g.registry == nil {
		return
	}
	for _, name := range names {
		g.registry.Unregister(name)
	}
}

var _ contract.StateListener = (*Gateway)(nil)
