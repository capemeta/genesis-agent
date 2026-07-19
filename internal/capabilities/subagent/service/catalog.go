// Package service 提供子智能体 Definition Catalog。
package service

import (
	"fmt"
	"sort"
	"strings"

	"genesis-agent/internal/capabilities/subagent/model"
	subagentprompt "genesis-agent/internal/capabilities/subagent/prompt"
)

// Catalog 是 Task 工具的定义查询端口。
type Catalog interface {
	Get(name string) (model.Definition, bool)
	List() []model.Summary
}

// MemoryCatalog 是 Phase 1 的只读 Catalog，后续可由多 Source 合并服务替代。
type MemoryCatalog struct{ definitions map[string]model.Definition }

// NewBuiltinCatalog 创建零配置可用的内置定义。
func NewBuiltinCatalog() *MemoryCatalog {
	return NewMemoryCatalog([]model.Definition{
		{Name: "explore", Description: "只读探索代码库并报告证据", WhenToUse: "需要搜索、定位实现或梳理调用链时", ReadOnly: true, SystemPrompt: "你是代码探索子智能体。只做只读检索和分析，给出带路径的简洁结论。"},
		{Name: "plan", Description: "只读分析并提出实施计划", WhenToUse: "需要在改动前厘清方案、风险和步骤时", ReadOnly: true, SystemPrompt: "你是规划子智能体。只做只读分析，输出可执行计划、依赖和风险。"},
		{Name: "general-purpose", Description: "处理边界清晰的独立子任务", WhenToUse: "任务可独立完成且主线程不需要保留全部中间上下文时", SystemPrompt: "你是通用子智能体。完成被委派的任务并返回简洁、可复用的结论。"},
	})
}

// NewMemoryCatalog 创建规范化后的只读 Catalog。
func NewMemoryCatalog(definitions []model.Definition) *MemoryCatalog {
	items := make(map[string]model.Definition, len(definitions))
	for _, definition := range definitions {
		definition.Name = strings.TrimSpace(definition.Name)
		if definition.Name != "" {
			items[definition.Name] = definition
		}
	}
	return &MemoryCatalog{definitions: items}
}

func (c *MemoryCatalog) Get(name string) (model.Definition, bool) {
	definition, ok := c.definitions[strings.TrimSpace(name)]
	return definition, ok
}

func (c *MemoryCatalog) List() []model.Summary {
	items := make([]model.Summary, 0, len(c.definitions))
	for _, definition := range c.definitions {
		items = append(items, model.Summary{Name: definition.Name, Description: definition.Description, WhenToUse: definition.WhenToUse})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

// DescriptionOptions 控制 Task 动态 Description 的姿态与并行提示。
type DescriptionOptions struct {
	Posture       string
	MaxConcurrent int
}

// RenderDescription 渲染动态工具描述，避免把 agent 名作为工具名暴露。
func RenderDescription(catalog Catalog, opts DescriptionOptions) (string, error) {
	if catalog == nil {
		return "委派独立子智能体。", fmt.Errorf("subagent Catalog不能为空")
	}
	agents := catalog.List()
	summaries := make([]subagentprompt.AgentSummary, 0, len(agents))
	for _, item := range agents {
		summaries = append(summaries, subagentprompt.AgentSummary{
			Name: item.Name, Description: item.Description, WhenToUse: item.WhenToUse,
		})
	}
	return subagentprompt.RenderToolDescription(summaries, subagentprompt.DescriptionOptions{
		Posture:       subagentprompt.NormalizePosture(opts.Posture),
		MaxConcurrent: opts.MaxConcurrent,
	})
}
