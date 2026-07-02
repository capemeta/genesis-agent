package domain

// AgentType Agent执行策略类型
type AgentType string

const (
	AgentTypeReactLoop   AgentType = "react_loop"
	AgentTypePlanExecute AgentType = "plan_execute"
)

// RuntimePolicy 运行时策略，控制迭代次数、Token预算、超时等安全边界
type RuntimePolicy struct {
	MaxIterations int   // 最大迭代次数，防止无限循环
	MaxTokens     int64 // Token预算上限（0=不限制）
	TimeoutSec    int   // 超时秒数（0=不限制）
}

// ToolRef 工具引用，指向已注册工具的名称
type ToolRef struct {
	Name string
}

// Agent 配置模板，定义Agent的行为策略
// 注意：Agent不持有运行状态，状态在 Run 和 RunContext 中
type Agent struct {
	ID            string
	TenantID      string
	Name          string
	Description   string
	Type          AgentType
	DefaultModel  string
	SystemPrompt  string
	Tools         []ToolRef // 允许使用的工具列表
	RuntimePolicy RuntimePolicy
}
