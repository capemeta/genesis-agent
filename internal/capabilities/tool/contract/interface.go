// Package tool 定义工具接口、元信息和参数Schema类型
// Tool是Agent与外部能力交互的标准方式，通过ToolRegistry统一管理
package tool

import "context"

// ParameterSchema 工具参数的JSON Schema定义
// 遵循 JSON Schema Draft-07 规范，与 OpenAI function calling 格式兼容
type ParameterSchema struct {
	Type        string                      `json:"type"`
	Description string                      `json:"description,omitempty"`
	Properties  map[string]*ParameterSchema `json:"properties,omitempty"`
	Required    []string                    `json:"required,omitempty"`
	Enum        []string                    `json:"enum,omitempty"`
	Items       *ParameterSchema            `json:"items,omitempty"` // 用于array类型
}

// ToolExposure 描述工具对模型和运行时的暴露方式。
type ToolExposure string

const (
	ToolExposureDirect   ToolExposure = "direct"
	ToolExposureDeferred ToolExposure = "deferred"
	ToolExposureHidden   ToolExposure = "hidden"
)

// ToolTraits 描述工具治理元数据，用于可见性、权限、调度和审计。
type ToolTraits struct {
	Exposure                ToolExposure `json:"exposure,omitempty"`
	ReadOnly                bool         `json:"read_only,omitempty"`
	ConcurrencySafe         bool         `json:"concurrency_safe,omitempty"`
	RequiresUserInteraction bool         `json:"requires_user_interaction,omitempty"`
	NeedsPermission         bool         `json:"needs_permission,omitempty"`
}

// Normalize 填充 traits 默认值。
func (t ToolTraits) Normalize() ToolTraits {
	if t.Exposure == "" {
		t.Exposure = ToolExposureDirect
	}
	return t
}

// Info 工具元信息，描述工具的能力和参数要求。
// LLM通过这些信息决定何时以及如何调用工具。
type Info struct {
	Name        string           // 工具名称（唯一标识符，snake_case）
	Description string           // 工具功能描述（供LLM理解）
	Parameters  *ParameterSchema // 工具参数 Schema（type=object）
	Traits      ToolTraits       // 工具治理元数据
}

// WithTraits 为工具信息设置治理元数据。
func WithTraits(info *Info, traits ToolTraits) *Info {
	if info == nil {
		return nil
	}
	info.Traits = traits.Normalize()
	return info
}

// TraitsOf 返回工具信息上的治理元数据。
func TraitsOf(info *Info) ToolTraits {
	if info == nil {
		return ToolTraits{Exposure: ToolExposureHidden}
	}
	if info.Traits == (ToolTraits{}) {
		return DefaultTraits(info.Name).Normalize()
	}
	return info.Traits.Normalize()
}

// DefaultTraits 返回内建工具的保守默认治理元数据。
func DefaultTraits(name string) ToolTraits {
	switch name {
	case "current_time", "calculator", "read_file", "list_dir", "walk_dir", "glob", "grep":
		return ToolTraits{Exposure: ToolExposureDirect, ReadOnly: true, ConcurrencySafe: true}
	case "search_skill_resources":
		return ToolTraits{Exposure: ToolExposureDirect, ReadOnly: true, ConcurrencySafe: true, NeedsPermission: true}
	case "write_file", "edit_file", "apply_patch":
		return ToolTraits{Exposure: ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false, NeedsPermission: true}
	case "read_skill_resource":
		return ToolTraits{Exposure: ToolExposureDirect, ReadOnly: true, ConcurrencySafe: true, NeedsPermission: true}
	case "load_skill":
		return ToolTraits{Exposure: ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false, NeedsPermission: true, RequiresUserInteraction: true}
	case "run_command", "http_request":
		return ToolTraits{Exposure: ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false, NeedsPermission: true}
	default:
		return ToolTraits{Exposure: ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false}
	}
}

// Tool 工具接口，所有内置和外部工具必须实现此接口
type Tool interface {
	// GetInfo 返回工具元信息，用于注册和向LLM声明能力
	GetInfo() *Info
	// Execute 执行工具，params为JSON格式的参数字符串
	// 返回结果为字符串（可以是JSON或纯文本），供LLM理解
	Execute(ctx context.Context, params string) (string, error)
}

// Registry 工具注册表接口
type Registry interface {
	// Register 注册一个工具，若名称重复则覆盖
	Register(t Tool)
	// Get 按名称获取工具，返回 nil 表示未找到
	Get(name string) Tool
	// Execute 执行指定工具，若工具不存在返回错误
	Execute(ctx context.Context, name, params string) (string, error)
	// ListInfos 返回所有已注册工具的元信息列表
	ListInfos() []*Info
	// FilterInfos 按工具名称列表过滤，返回指定工具的元信息
	FilterInfos(names []string) []*Info
	// Names 返回所有已注册工具的名称列表
	Names() []string
}
