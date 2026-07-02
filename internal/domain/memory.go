package domain

import "time"

// MemoryEntry 长期记忆领域模型
type MemoryEntry struct {
	ID             string
	TenantID       string
	UserID         string
	WorkspaceID    string
	Content        string
	Tags           []string
	Importance     int // 记忆重要性权重 (1-10)
	AccessCount    int // 访问/命中次数
	LastAccessedAt time.Time
	RuntimeAudit
}
