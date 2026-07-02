package app

import (
	"context"
	"fmt"
	"time"

	"genesis-agent/internal/domain"
)

// ClearSession 清空指定会话的短期记忆历史
func (s *agentServiceImpl) ClearSession(ctx context.Context, sessionID string) error {
	return s.memStore.ClearHistory(ctx, sessionID)
}

// NewSession 创建新的对话会话
func (s *agentServiceImpl) NewSession() *domain.Session {
	return &domain.Session{
		ID:        fmt.Sprintf("session-%d", time.Now().UnixNano()),
		TenantID:  "dev",
		AgentID:   s.defaultAgent.ID,
		UserID:    "user",
		CreatedAt: time.Now(),
	}
}
