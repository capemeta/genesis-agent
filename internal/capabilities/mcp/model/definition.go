package model

import "encoding/json"

// DefinitionOrigin 标记 server 定义来源。
type DefinitionOrigin string

const (
	OriginBuiltin     DefinitionOrigin = "builtin"
	OriginConfig      DefinitionOrigin = "config"
	OriginUser        DefinitionOrigin = "user"
	OriginProject     DefinitionOrigin = "project"
	OriginMarketplace DefinitionOrigin = "marketplace"
	OriginSession     DefinitionOrigin = "session"
)

// McpServerDefinition 是 Catalog 合并后的 server 定义。
type McpServerDefinition struct {
	Config     McpServerConfig
	Origin     DefinitionOrigin
	Precedence int
	// ConfigKey 用于检测同名 server 配置变更（变更则重建连接）。
	ConfigKey string
	// OverriddenBy 记录冲突消解时被覆盖的来源（审计用）。
	OverriddenOrigins []DefinitionOrigin
	// DisabledReason 企业 Requirements 或审批拒绝时的禁用原因。
	DisabledReason string
}

// McpToolRef 是路由用的 (server, originalTool) 引用。
type McpToolRef struct {
	ServerName string
	ToolName   string
}

// ToolSnapshot 是 tools/list 的缓存条目。
type ToolSnapshot struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	ReadOnlyHint *bool           `json:"read_only_hint,omitempty"`
}

// ToolResult 是 tools/call 的归一化结果。
type ToolResult struct {
	Content string
	IsError bool
	RawJSON json.RawMessage
}

// ResourceSnapshot 是 resources/list 条目。
type ResourceSnapshot struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
}
