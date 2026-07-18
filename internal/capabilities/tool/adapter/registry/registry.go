// Package registry 提供内存工具注册表实现。
package registry

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"genesis-agent/internal/capabilities/tool/contract"
)

// Registry 工具注册表，线程安全。
// 所有工具在启动时注册，运行时只读。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]entry // key为工具名称
}

type entry struct {
	tool  tool.Tool
	owner string
}

// NewRegistry 创建空的工具注册表。
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]entry),
	}
}

// Register 注册一个工具，名称重复时拒绝覆盖。
func (r *Registry) Register(t tool.Tool) error {
	if t == nil || t.GetInfo() == nil {
		return fmt.Errorf("注册工具失败: tool/info 不能为空")
	}
	info := t.GetInfo()
	if strings.TrimSpace(info.Name) == "" || strings.TrimSpace(info.Name) != info.Name {
		return fmt.Errorf("注册工具失败: name 不能为空或包含首尾空白")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, exists := r.tools[info.Name]; exists {
		return fmt.Errorf("工具 %q 已由 %q 注册，拒绝由 %q 静默覆盖", info.Name, current.owner, registrationOwner(t))
	}
	r.tools[info.Name] = entry{tool: t, owner: registrationOwner(t)}
	return nil
}

// Replace 在 owner 符合预期时显式替换已注册工具。
func (r *Registry) Replace(name, expectedOwner string, t tool.Tool) error {
	if t == nil || t.GetInfo() == nil {
		return fmt.Errorf("替换工具 %q 失败: tool/info 不能为空", name)
	}
	if t.GetInfo().Name != name {
		return fmt.Errorf("替换工具 %q 失败: 新工具名称为 %q", name, t.GetInfo().Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.tools[name]
	if !exists {
		return fmt.Errorf("替换工具 %q 失败: 工具尚未注册", name)
	}
	if current.owner != expectedOwner {
		return fmt.Errorf("替换工具 %q 失败: owner=%q，期望=%q", name, current.owner, expectedOwner)
	}
	r.tools[name] = entry{tool: t, owner: registrationOwner(t)}
	return nil
}

// Owner 返回工具注册来源。
func (r *Registry) Owner(name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	registered, ok := r.tools[name]
	return registered.owner, ok
}

// Unregister 按名称移除工具；不存在时为 no-op。
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

// Get 按名称获取工具，返回 nil 表示未找到。
func (r *Registry) Get(name string) tool.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name].tool
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
	for _, registered := range r.tools {
		infos = append(infos, registered.tool.GetInfo())
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
		if registered, ok := r.tools[name]; ok {
			infos = append(infos, registered.tool.GetInfo())
		}
	}
	return infos
}

func registrationOwner(t tool.Tool) string {
	typ := reflect.TypeOf(t)
	if typ == nil {
		return "unknown"
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if pkg := typ.PkgPath(); pkg != "" {
		return pkg
	}
	return typ.String()
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
