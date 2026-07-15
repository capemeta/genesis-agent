package domain

import "time"

// RunStatus Run的生命周期状态
type RunStatus string

const (
	RunStatusCreated   RunStatus = "created"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

// TokenUsage LLM Token用量统计
type TokenUsage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

// Run 一次完整的自主执行过程，包含多个Step
// 对应 AGENTS.md §3.4 Run 定义
type Run struct {
	ID          string
	TenantID    string
	SessionID   string
	Status      RunStatus
	FinalAnswer string  // Loop结束时的最终回答
	Incomplete  bool    // partial_complete：以当前最佳结果结束，结果可能不完整
	TotalTokens int64   // 本次Run的Token总消耗
	Steps       []*Step // 按执行顺序排列的步骤列表
	StartedAt   time.Time
	FinishedAt  *time.Time
}

// StartRunRequest 启动一次Run的请求参数
type StartRunRequest struct {
	SessionID       string
	TenantID        string
	UserInput       string
	Agent           *Agent
	ContextStrategy string // 新增：本次会话的上下文预算策略（如 default/rag/coding）
}
