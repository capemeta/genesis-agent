// Package capability 把 Capability Registry 中的 tool capability 投影为 Tool Gateway 工具。
package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	capcontract "genesis-agent/internal/capabilities/capability/contract"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

// Adapter 实现 Capability RuntimeAdapter，并把 tool capability 注册到工具注册表。
type Adapter struct {
	mu       sync.RWMutex
	registry tool.Registry
	tools    map[string]*capabilityTool
}

// New 创建 Tool capability adapter。registry 可为空；为空时调用方可通过 Tools 取出工具自行注册。
func New(registry tool.Registry) *Adapter {
	return &Adapter{registry: registry, tools: map[string]*capabilityTool{}}
}

func (a *Adapter) CapabilityType() capmodel.CapabilityType { return capmodel.CapabilityTypeTool }

func (a *Adapter) Register(ctx context.Context, capability capmodel.CapabilityIndexRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if capability.Type != capmodel.CapabilityTypeTool {
		return fmt.Errorf("tool adapter不支持capability type: %s", capability.Type)
	}
	name := strings.TrimSpace(capability.Name)
	if name == "" {
		name = strings.TrimSpace(capability.ID)
	}
	if name == "" {
		return fmt.Errorf("tool capability缺少name和id")
	}
	wrapped := &capabilityTool{capability: capability}
	a.mu.RLock()
	previous := a.tools[capability.ID]
	a.mu.RUnlock()
	if a.registry != nil {
		if previous != nil && previous.GetInfo().Name == name && a.registry.Get(name) == previous {
			owner, ok := a.registry.Owner(name)
			if !ok {
				return fmt.Errorf("更新 tool capability %q: registry owner 缺失", capability.ID)
			}
			if err := a.registry.Replace(name, owner, wrapped); err != nil {
				return fmt.Errorf("更新 tool capability %q: %w", capability.ID, err)
			}
		} else {
			if err := a.registry.Register(wrapped); err != nil {
				return fmt.Errorf("注册 tool capability %q: %w", capability.ID, err)
			}
			if previous != nil {
				a.registry.Unregister(previous.GetInfo().Name)
			}
		}
	}
	if previous != nil {
		previous.setEnabled(false)
	}
	a.mu.Lock()
	a.tools[capability.ID] = wrapped
	a.mu.Unlock()
	return nil
}

func (a *Adapter) Unregister(ctx context.Context, capability capmodel.CapabilityIndexRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	wrapped := a.tools[capability.ID]
	if wrapped != nil {
		wrapped.setEnabled(false)
	}
	delete(a.tools, capability.ID)
	a.mu.Unlock()
	if wrapped != nil && a.registry != nil && a.registry.Get(wrapped.GetInfo().Name) == wrapped {
		a.registry.Unregister(wrapped.GetInfo().Name)
	}
	return nil
}

func (a *Adapter) SetEnabled(ctx context.Context, capability capmodel.CapabilityIndexRecord, enabled bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.RLock()
	wrapped := a.tools[capability.ID]
	a.mu.RUnlock()
	if wrapped == nil {
		registered := capability
		registered.Enabled = enabled
		return a.Register(ctx, registered)
	}
	wrapped.setEnabled(enabled)
	return nil
}

// RegisterCapabilities 注册一组 tool capability，通常用于产品启动时从 Capability Registry 恢复安装态工具。
func (a *Adapter) RegisterCapabilities(ctx context.Context, capabilities []capmodel.CapabilityIndexRecord) error {
	for _, capability := range capabilities {
		if capability.Type != capmodel.CapabilityTypeTool {
			continue
		}
		if err := a.Register(ctx, capability); err != nil {
			return err
		}
	}
	return nil
}

// Tools 返回 adapter 当前持有的工具快照。
func (a *Adapter) Tools() []tool.Tool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]tool.Tool, 0, len(a.tools))
	for _, t := range a.tools {
		out = append(out, t)
	}
	return out
}

// LoadFromRegistry 从 Capability Registry 读取 tool capability 并注册。
func (a *Adapter) LoadFromRegistry(ctx context.Context, registry capcontract.Registry) error {
	if registry == nil {
		return nil
	}
	records, err := registry.ListCapabilities(ctx, capmodel.CapabilityQuery{Types: []capmodel.CapabilityType{capmodel.CapabilityTypeTool}, IncludeDisabled: true})
	if err != nil {
		return err
	}
	return a.RegisterCapabilities(ctx, records)
}

type capabilityTool struct {
	mu         sync.RWMutex
	capability capmodel.CapabilityIndexRecord
}

func (t *capabilityTool) GetInfo() *tool.Info {
	t.mu.RLock()
	capability := t.capability
	t.mu.RUnlock()
	name := strings.TrimSpace(capability.Name)
	if name == "" {
		name = capability.ID
	}
	description := strings.TrimSpace(capability.Description)
	if description == "" {
		description = "Installed tool capability from " + capability.Spec
	}
	traits := tool.ToolTraits{
		Exposure:        exposureFromMetadata(capability.ManifestMetadata, capability.Enabled),
		ReadOnly:        boolFromMetadata(capability.ManifestMetadata, "read_only"),
		ConcurrencySafe: boolFromMetadata(capability.ManifestMetadata, "concurrency_safe"),
		NeedsPermission: boolFromMetadata(capability.ManifestMetadata, "needs_permission"),
	}
	if !capability.Enabled {
		traits.Exposure = tool.ToolExposureHidden
	}
	return &tool.Info{
		Name:        name,
		Description: description,
		Parameters:  parametersFromMetadata(capability.ManifestMetadata),
		Traits:      traits.Normalize(),
	}
}

func (t *capabilityTool) Execute(ctx context.Context, params string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	t.mu.RLock()
	capability := t.capability
	t.mu.RUnlock()
	if !capability.Enabled {
		return "", fmt.Errorf("tool capability %s 已禁用", capability.ID)
	}
	return "", fmt.Errorf("tool capability %s 已注册到Tool Gateway，但runtime=%q entrypoint=%q 尚未接入执行器", capability.ID, capability.Runtime, capability.Entrypoint)
}

func (t *capabilityTool) setEnabled(enabled bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.capability.Enabled = enabled
}

func exposureFromMetadata(metadata map[string]any, enabled bool) tool.ToolExposure {
	if !enabled {
		return tool.ToolExposureHidden
	}
	value, _ := metadata["exposure"].(string)
	switch tool.ToolExposure(strings.TrimSpace(value)) {
	case tool.ToolExposureDeferred:
		return tool.ToolExposureDeferred
	case tool.ToolExposureHidden:
		return tool.ToolExposureHidden
	default:
		return tool.ToolExposureDirect
	}
}

func boolFromMetadata(metadata map[string]any, key string) bool {
	value, ok := metadata[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func parametersFromMetadata(metadata map[string]any) *tool.ParameterSchema {
	raw, ok := metadata["parameters"]
	if !ok {
		return &tool.ParameterSchema{Type: "object"}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return &tool.ParameterSchema{Type: "object"}
	}
	var schema tool.ParameterSchema
	if err := json.Unmarshal(data, &schema); err != nil || schema.Type == "" {
		return &tool.ParameterSchema{Type: "object"}
	}
	return &schema
}
