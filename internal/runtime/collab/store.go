package collab

import (
	"context"
	"sync"
)

// SessionState 会话级协作状态（可持久化）。
type SessionState struct {
	Mode           Mode `json:"mode"`
	HandoffPending bool `json:"handoff_pending,omitempty"`
}

// Store 协作模式持久化 Port（CLI 文件 / Desktop / Enterprise DB 可替换）。
type Store interface {
	Get(ctx context.Context, sessionID string) (SessionState, error)
	Set(ctx context.Context, sessionID string, state SessionState) error
}

// MemoryStore 进程内 Store，供测试与未注入产品时使用。
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]SessionState
}

// NewMemoryStore 创建内存 Store。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]SessionState)}
}

func (s *MemoryStore) Get(_ context.Context, sessionID string) (SessionState, error) {
	if s == nil || sessionID == "" {
		return SessionState{Mode: ModeDefault}, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.data[sessionID]
	if !ok {
		return SessionState{Mode: ModeDefault}, nil
	}
	st.Mode = Normalize(st.Mode)
	return st, nil
}

func (s *MemoryStore) Set(_ context.Context, sessionID string, state SessionState) error {
	if s == nil {
		return nil
	}
	if sessionID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = make(map[string]SessionState)
	}
	state.Mode = Normalize(state.Mode)
	s.data[sessionID] = state
	return nil
}
