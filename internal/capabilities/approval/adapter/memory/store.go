// Package memory 提供内存审批授权缓存。
package memory

import (
	"context"
	"encoding/json"
	"sync"

	"genesis-agent/internal/capabilities/approval/model"
)

// Store 是进程内审批授权缓存。
type Store struct {
	mu   sync.RWMutex
	data map[string]model.Decision
}

// NewStore 创建内存 store。
func NewStore() *Store {
	return &Store{data: make(map[string]model.Decision)}
}

// Get 查询授权缓存。
func (s *Store) Get(ctx context.Context, key model.GrantKey) (model.Decision, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.Decision{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	decision, ok := s.data[stableKey(key)]
	return decision, ok, nil
}

// Put 写入授权缓存。once 不缓存。
func (s *Store) Put(ctx context.Context, key model.GrantKey, decision model.Decision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key.Scope == "" || key.Scope == model.GrantScopeOnce {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[stableKey(key)] = decision
	return nil
}

func stableKey(key model.GrantKey) string {
	data, err := json.Marshal(key)
	if err != nil {
		return string(key.Action) + "|" + key.ResourceURI + "|" + string(key.Scope)
	}
	return string(data)
}
