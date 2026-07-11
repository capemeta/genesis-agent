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

	// MaxConsecutiveFail 任意工具连续失败上限（不要求同 args）；0=关闭。
	MaxConsecutiveFail int

	// Repeat Guard（见 docs/superpowers/specs/2026-07-10-agent-repeated-failure-circuit-design.md）
	// 指针字段 nil 表示使用平台默认：Enabled=true, MaxIdentical=2, MaxStagnant=5。
	RepeatGuardEnabled       *bool
	MaxIdenticalToolFailures *int // 同 call_key 连续失败达该值后，下一次拦截；显式 0=关闭 L1
	MaxStagnantIterations    *int // 连续无进展迭代达该值触发 L2；显式 0=关闭 L2
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
