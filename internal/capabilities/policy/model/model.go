// Package model 定义策略能力的扩展上下文模型。
package model

// Subject 描述策略评估主体。第一批由 approval request metadata 间接携带，后续再显式接入。
type Subject struct {
	TenantID  string
	UserID    string
	Roles     []string
	ProjectID string
	AgentID   string
	Product   string
}

// Environment 描述策略评估环境。第一批保留结构，后续由产品 bootstrap 注入。
type Environment struct {
	Profile       string
	WorkspaceID   string
	WorkspaceRoot string
	SandboxMode   string
	NetworkMode   string
	RuntimeEnv    string
}
