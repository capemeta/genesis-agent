// Package registry 提供内存工具注册表实现。
package registry

import (
	"context"
	"fmt"
	"sync"

	"genesis-agent/internal/capabilities/tool/contract"
)

// Registry 工具注册表，线程安全。
// 所有工具在启动时注册，运行时只读。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]tool.Tool // key为工具名称
}

// NewRegistry 创建空的工具注册表。
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]tool.Tool),
	}
}

// Register 注册一个工具，若名称重复则覆盖。
func (r *Registry) Register(t tool.Tool) {
	info := t.GetInfo()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[info.Name] = t
}

// Get 按名称获取工具，返回 nil 表示未找到。
func (r *Registry) Get(name string) tool.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// Execute 执行指定工具，若工具不存在返回错误。
func (r *Registry) Execute(ctx context.Context, name, params string) (string, error) {
	t := r.Get(name)
	if t == nil {
		return "", fmt.Errorf("工具 [%s] 未注册", name)
	}
	return t.Execute(ctx, params)
}

// ListInfos 返回所有已注册工具的元信息列表。
func (r *Registry) ListInfos() []*tool.Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	infos := make([]*tool.Info, 0, len(r.tools))
	for _, t := range r.tools {
		infos = append(infos, t.GetInfo())
	}
	return infos
}

// FilterInfos 按工具名称列表过滤，返回指定工具的元信息。
// 用于 Agent 运行时只向 LLM 暴露允许使用的工具子集。
func (r *Registry) FilterInfos(names []string) []*tool.Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	infos := make([]*tool.Info, 0, len(names))
	for _, name := range names {
		if t, ok := r.tools[name]; ok {
			infos = append(infos, t.GetInfo())
		}
	}
	return infos
}

// Names 返回所有已注册工具的名称列表。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}
