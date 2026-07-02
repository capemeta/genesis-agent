// Package memory 定义记忆系统接口
// 对应 AGENTS.md §5.4 记忆系统（四层）
// MVP阶段实现 ShortTermMemory（会话级对话历史）
package memory

import (
	"context"

	"genesis-agent/internal/domain"
)

// ShortTermStore 短期记忆接口，管理Session级别的对话历史
// 生命周期与Session相同，跨Run保存
// 使用我们自定义的 domain.Message，不依赖任何外部框架
type ShortTermStore interface {
	// AppendMessages 追加消息到Session历史
	AppendMessages(ctx context.Context, sessionID string, messages []*domain.Message) error
	// GetHistory 获取Session的完整对话历史
	GetHistory(ctx context.Context, sessionID string) ([]*domain.Message, error)
	// ClearHistory 清空Session历史（用于开始新对话）
	ClearHistory(ctx context.Context, sessionID string) error
}
