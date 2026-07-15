package contract

import (
	"context"

	"genesis-agent/internal/capabilities/mcp/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
)

// RuntimeCatalogEnv 是 Catalog 合并时的运行时上下文。
type RuntimeCatalogEnv struct {
	Channel     profilemodel.ChannelType
	TenantID    string
	ProjectID   string
	AgentID     string
	UserID      string
	RoleIDs     []string
	Environment profilemodel.RuntimeEnvironment
	Workspace   string
}

// DefinitionSource 是 server 定义来源（对齐 Codex Extension Contributor）。
type DefinitionSource interface {
	// List 返回本来源的 server 定义；Origin 标记来源。
	List(ctx context.Context, env RuntimeCatalogEnv) ([]model.McpServerDefinition, error)
	Precedence() int
}

// RequirementsFilter 在 Catalog 合并后、Manager.Sync 前强制禁用不合规 server。
type RequirementsFilter interface {
	Filter(ctx context.Context, defs []model.McpServerDefinition) ([]model.McpServerDefinition, error)
}

// Catalog 多来源合并端口。
type Catalog interface {
	Merge(ctx context.Context, env RuntimeCatalogEnv) ([]model.McpServerDefinition, error)
}
