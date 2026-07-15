package store

import (
	"context"
	"sync"

	"genesis-agent/internal/capabilities/mcp/contract"
)

// MemoryApprovalStore 内存实现的 project 预连接审批存储。
type MemoryApprovalStore struct {
	mu   sync.RWMutex
	data map[string]contract.ApprovalDecision
}

// NewMemory 创建内存审批存储。
func NewMemory() *MemoryApprovalStore {
	return &MemoryApprovalStore{data: make(map[string]contract.ApprovalDecision)}
}

func (s *MemoryApprovalStore) Get(ctx context.Context, serverName string) (contract.ApprovalDecision, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[serverName]
	return v, ok, nil
}

func (s *MemoryApprovalStore) Put(ctx context.Context, serverName string, decision contract.ApprovalDecision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[serverName] = decision
	return nil
}

var _ contract.ApprovalStore = (*MemoryApprovalStore)(nil)
