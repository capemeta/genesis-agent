// Package gateway 提供工具调用网关。
package gateway

import (
	"context"
	"fmt"
	"sort"
	"strings"

	profilemodel "genesis-agent/internal/capabilities/profile/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

// Gateway 在工具注册表外层执行可见性过滤和调用前校验。
type Gateway struct {
	registry tool.Registry
	tools    profilemodel.ToolSet
}

// New 创建工具网关。
func New(registry tool.Registry, tools profilemodel.ToolSet) *Gateway {
	return &Gateway{registry: registry, tools: tools}
}

// Register 透传工具注册。产品 bootstrap 仍应优先注册后再创建 Gateway。
func (g *Gateway) Register(t tool.Tool) {
	g.registry.Register(t)
}

// Get 按名称获取已允许的工具。
func (g *Gateway) Get(name string) tool.Tool {
	if !g.isAllowed(name) {
		return nil
	}
	return g.registry.Get(name)
}

// Execute 执行工具，并先做产品能力策略校验。
func (g *Gateway) Execute(ctx context.Context, name, params string) (string, error) {
	if !g.isAllowed(name) {
		return "", fmt.Errorf("工具 [%s] 未被当前产品 Profile 允许", name)
	}
	return g.registry.Execute(ctx, name, params)
}

// ListInfos 返回当前 Profile 可见的工具列表。
func (g *Gateway) ListInfos() []*tool.Info {
	infos := g.registry.ListInfos()
	allowed := make([]*tool.Info, 0, len(infos))
	for _, info := range infos {
		if g.isAllowed(info.Name) {
			allowed = append(allowed, info)
		}
	}
	sort.Slice(allowed, func(i, j int) bool { return allowed[i].Name < allowed[j].Name })
	return allowed
}

// FilterInfos 返回指定名称中被当前 Profile 允许的工具元信息。
func (g *Gateway) FilterInfos(names []string) []*tool.Info {
	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if g.isAllowed(name) {
			filtered = append(filtered, name)
		}
	}
	infos := g.registry.FilterInfos(filtered)
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos
}

// Names 返回当前 Profile 可见的工具名。
func (g *Gateway) Names() []string {
	names := g.registry.Names()
	allowed := make([]string, 0, len(names))
	for _, name := range names {
		if g.isAllowed(name) {
			allowed = append(allowed, name)
		}
	}
	sort.Strings(allowed)
	return allowed
}

func (g *Gateway) isAllowed(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if matchesAny(g.tools.Disabled, name) {
		return false
	}
	if len(g.tools.Enabled) == 0 {
		return true
	}
	return matchesAny(g.tools.Enabled, name)
}

func matchesAny(patterns []string, name string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == "*" || pattern == name {
			return true
		}
		if strings.HasSuffix(pattern, ".*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
	}
	return false
}
