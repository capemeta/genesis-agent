// Package inmemory 提供基于内存的短期记忆实现。
package inmemory

import (
	"context"
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
func NewInMemoryStore() contract.ShortTermStore {
	return &InMemoryStore{sessions: make(map[string][]*domain.Message)}
}

func (s *InMemoryStore) AppendMessages(_ context.Context, sessionID string, messages []*domain.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = append(s.sessions[sessionID], messages...)
	return nil
}

func (s *InMemoryStore) GetHistory(_ context.Context, sessionID string) ([]*domain.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	history := s.sessions[sessionID]
	if len(history) == 0 {
		return nil, nil
	}
	result := make([]*domain.Message, len(history))
	copy(result, history)
	return result, nil
}

func (s *InMemoryStore) ClearHistory(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	return nil
}
