package domain

import "time"

// SessionState 会话生命周期状态类型
type SessionState string

const (
	SessionStateCreate     SessionState = "create"     // 首次创建
	SessionStateActive     SessionState = "active"     // 有活跃的 Run
	SessionStateCompacting SessionState = "compacting" // 正在进行 auto-compact
	SessionStateIdle       SessionState = "idle"       // 无活跃 Run，但未归档
	SessionStateArchived   SessionState = "archived"   // 会话归档（短期记忆迁冷层）
	SessionStateDeleted    SessionState = "deleted"    // 软删除状态
)

// Session 对话上下文，关联用户与Agent的一次对话会话
type Session struct {
	ID              string       `json:"id"`
	TenantID        string       `json:"tenant_id"`
	AgentID         string       `json:"agent_id"`
	UserID          string       `json:"user_id"`
	Title           string       `json:"title"`
	Status          SessionState `json:"status"`            // 新增：会话状态
	ContextStrategy string       `json:"context_strategy"`   // 新增：选择的预算 Profile 名称
	TotalTokens     int64        `json:"total_tokens"`       // 新增：累计 Token 消耗
	SummaryLeafID   string       `json:"summary_leaf_id"`   // 新增：最后一次压缩对应的消息 uuid
	AppID           string       `json:"app_id,omitempty"`   // 新增：多智能体 App ID
	AgentInstanceID string       `json:"agent_instance_id,omitempty"` // 新增：多智能体 Agent 实例 ID
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

// SessionSummary 会话摘要记录（对应持久层 .summary.json）
// Rolling Summary 实现：压缩时把旧 Content 全文注入压缩提示词的 {previous_summary} 占位，
// 由 LLM 产出综合新摘要，确保最早期信息不随多次压缩丢失。
type SessionSummary struct {
	SessionID   string    `json:"session_id"`   // 所属会话 ID
	Content     string    `json:"content"`      // 摘要正文（Markdown，结构化分段）
	LeafID      string    `json:"leaf_id"`      // 对应 JSONL envelope 的 uuid——resume 时从此处截断，之前用本摘要代表
	TokensCount int       `json:"tokens_count"` // 摘要自身的估算 token 数
	Iteration   int       `json:"iteration"`    // 压缩次数（1-based，用于 rolling summary 调试与监控）
	CreatedAt   time.Time `json:"created_at"`
}

