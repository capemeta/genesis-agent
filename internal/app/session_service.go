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

// ListSessionMessages 返回短期记忆完整链（EnsureKind）；投影由产品侧完成。
func (s *agentServiceImpl) ListSessionMessages(ctx context.Context, sessionID string) ([]*domain.Message, error) {
	msgs, err := s.memStore.GetHistory(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session messages: %w", err)
	}
	for _, m := range msgs {
		if m != nil {
			m.EnsureKind()
		}
	}
	return msgs, nil
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
