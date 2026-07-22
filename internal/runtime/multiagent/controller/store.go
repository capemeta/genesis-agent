package controller

import (
	"context"
	"fmt"
	"sync"

	"genesis-agent/internal/runtime/multiagent/contract"
)

type memoryStore struct {
	mu     sync.RWMutex
	values map[string]contract.StoredInstance
}

func newMemoryStore() *memoryStore {
	return &memoryStore{values: make(map[string]contract.StoredInstance)}
}

func (s *memoryStore) Save(_ context.Context, value contract.StoredInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[value.Instance.AgentID] = value
	return nil
}

func (s *memoryStore) SaveIfInvocationAbsent(_ context.Context, value contract.StoredInstance) (contract.StoredInstance, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bindingID := value.Request.InvocationBinding.ID
	if bindingID != "" {
		for _, existing := range s.values {
			if existing.Request.TenantID == value.Request.TenantID && existing.Request.ParentRunID == value.Request.ParentRunID && existing.Request.InvocationBinding.ID == bindingID {
				return existing, false, nil
			}
		}
	}
	s.values[value.Instance.AgentID] = value
	return value, true, nil
}

func (s *memoryStore) Get(_ context.Context, agentID string) (contract.StoredInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.values[agentID]
	if !ok {
		return contract.StoredInstance{}, fmt.Errorf("subagent %q 不存在", agentID)
	}
	return value, nil
}
