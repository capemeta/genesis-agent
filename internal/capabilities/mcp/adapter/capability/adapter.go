package capability

import (
	"context"
	"fmt"
	"sync"

	capmodel "genesis-agent/internal/capabilities/capability/model"
	"genesis-agent/internal/capabilities/mcp/catalog"
	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
)

// Adapter 实现 capability.RuntimeAdapter（type=mcp）。
// 作为 Catalog 的一个 DefinitionSource 驱动者：Register/Unregister/SetEnabled → Manager.Sync。
type Adapter struct {
	mu      sync.Mutex
	manager contract.Manager
	catalog contract.Catalog
	env     contract.RuntimeCatalogEnv
	defs    map[string]model.McpServerDefinition
	// OnDefinitions 在 Sync 前回调最新合并定义（供 Gateway 刷新暴露策略等）。
	OnDefinitions func(defs []model.McpServerDefinition)
}

// New 创建 MCP RuntimeAdapter。
func New(manager contract.Manager, cat contract.Catalog, env contract.RuntimeCatalogEnv) *Adapter {
	return &Adapter{
		manager: manager,
		catalog: cat,
		env:     env,
		defs:    make(map[string]model.McpServerDefinition),
	}
}

func (a *Adapter) CapabilityType() capmodel.CapabilityType { return capmodel.CapabilityTypeMCP }

func (a *Adapter) Register(ctx context.Context, capability capmodel.CapabilityIndexRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if capability.Type != capmodel.CapabilityTypeMCP {
		return fmt.Errorf("mcp adapter 不支持 capability type: %s", capability.Type)
	}
	def, err := catalog.DefinitionFromRecord(capability)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.defs[capability.ID] = def
	a.mu.Unlock()
	return a.resync(ctx)
}

func (a *Adapter) Unregister(ctx context.Context, capability capmodel.CapabilityIndexRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.defs, capability.ID)
	a.mu.Unlock()
	return a.resync(ctx)
}

func (a *Adapter) SetEnabled(ctx context.Context, capability capmodel.CapabilityIndexRecord, enabled bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	def, ok := a.defs[capability.ID]
	if !ok {
		a.mu.Unlock()
		capability.Enabled = enabled
		return a.Register(ctx, capability)
	}
	def.Config.Enabled = enabled
	a.defs[capability.ID] = def
	a.mu.Unlock()
	return a.resync(ctx)
}

func (a *Adapter) resync(ctx context.Context) error {
	if a.manager == nil {
		return nil
	}
	var defs []model.McpServerDefinition
	if a.catalog != nil {
		merged, err := a.catalog.Merge(ctx, a.env)
		if err != nil {
			return err
		}
		defs = merged
	} else {
		a.mu.Lock()
		defs = make([]model.McpServerDefinition, 0, len(a.defs))
		for _, d := range a.defs {
			defs = append(defs, d)
		}
		a.mu.Unlock()
	}
	if a.OnDefinitions != nil {
		a.OnDefinitions(defs)
	}
	// 热更新走异步连接，避免 marketplace Install/SetEnabled 阻塞调用方。
	_, err := a.manager.SyncAsync(ctx, defs)
	return err
}
