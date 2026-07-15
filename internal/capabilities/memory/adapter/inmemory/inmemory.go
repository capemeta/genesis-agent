// Package inmemory 提供基于内存的短期记忆实现。
package inmemory

import (
	"context"
	"fmt"
	"sync"

	contract "genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
)

// InMemoryStore 基于内存的短期记忆存储，线程安全。
type InMemoryStore struct {
	mu       sync.RWMutex
	sessions map[string][]*domain.Message
}

// NewInMemoryStore 创建内存短期记忆存储。
func NewInMemoryStore() contract.ShortTermMemory {
	return &InMemoryStore{sessions: make(map[string][]*domain.Message)}
}

// Append 追加消息到 Session 历史
func (s *InMemoryStore) Append(_ context.Context, ref contract.SessionRef, messages []*domain.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[ref.SessionID] = append(s.sessions[ref.SessionID], messages...)
	return nil
}

// GetRecent 从新到旧返回消息，受条数与 token 预算约束
func (s *InMemoryStore) GetRecent(_ context.Context, ref contract.SessionRef, opt contract.RecentOptions) (contract.RecentResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history := s.sessions[ref.SessionID]
	if len(history) == 0 {
		return contract.RecentResult{}, nil
	}

	// 复制历史
	copied := make([]*domain.Message, len(history))
	copy(copied, history)

	// 极简实现：如果有 MaxTurns 且超出，做截断
	if opt.MaxTurns > 0 {
		// 计算实际用户轮数 (Kind = user_turn)
		userTurnsCount := 0
		for i := len(copied) - 1; i >= 0; i-- {
			if copied[i].NormalizedKind() == domain.MessageKindUserTurn {
				userTurnsCount++
				if userTurnsCount > opt.MaxTurns {
					// 发现超出的那一轮，截断之前的所有消息
					// 并标记 Truncated = true
					return contract.RecentResult{
						Messages:  copied[i+1:],
						Truncated: true,
					}, nil
				}
			}
		}
	}

	return contract.RecentResult{
		Messages: copied,
	}, nil
}

// Summarize 内存临时实现不执行真正的 LLM 摘要
func (s *InMemoryStore) Summarize(_ context.Context, ref contract.SessionRef, opt contract.SummarizeOptions) (contract.SummaryResult, error) {
	return contract.SummaryResult{}, nil
}

// GetSummary 内存临时实现返回空
func (s *InMemoryStore) GetSummary(_ context.Context, ref contract.SessionRef) (*domain.SessionSummary, error) {
	return nil, nil
}

// Clear 清空 Session 历史
func (s *InMemoryStore) Clear(_ context.Context, ref contract.SessionRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, ref.SessionID)
	return nil
}

func (s *InMemoryStore) Replay(ctx context.Context, ref contract.SessionRef, leafID string) ([]*domain.Message, error) {
	res, err := s.GetRecent(ctx, ref, contract.RecentOptions{})
	if err != nil || leafID == "" {
		return res.Messages, err
	}
	for i, message := range res.Messages {
		if message != nil && message.UUID == leafID {
			return res.Messages[:i+1], nil
		}
	}
	return nil, fmt.Errorf("replay leaf %q not found", leafID)
}

func (s *InMemoryStore) Fork(ctx context.Context, source, target contract.SessionRef, leafID string) error {
	messages, err := s.Replay(ctx, source, leafID)
	if err != nil {
		return err
	}
	cloned := make([]*domain.Message, 0, len(messages))
	for _, message := range messages {
		if message != nil {
			copy := *message
			copy.UUID = ""
			cloned = append(cloned, &copy)
		}
	}
	return s.Append(ctx, target, cloned)
}
