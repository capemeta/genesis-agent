package domain

import "time"

// Session 对话上下文，关联用户与Agent的一次对话会话
type Session struct {
	ID        string
	TenantID  string
	AgentID   string
	UserID    string
	Title     string
	CreatedAt time.Time
}
