// Package memory 提供并发安全的 Handoff Store 测试实现。
package memory

import (
	"context"
	"fmt"
	"sync"

	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/runtime/multiagent/handoff"
)

type Store struct {
	mu    sync.RWMutex
	items map[string]handoff.Receipt
	ids   map[string]string
}

func New() *Store {
	return &Store{items: make(map[string]handoff.Receipt), ids: make(map[string]string)}
}

func (s *Store) PutIfAbsent(ctx context.Context, receipt handoff.Receipt) (handoff.Receipt, bool, error) {
	if err := ctx.Err(); err != nil {
		return handoff.Receipt{}, false, err
	}
	if receipt.TenantID == "" || receipt.IdempotencyKey == "" || receipt.ID == "" {
		return handoff.Receipt{}, false, fmt.Errorf("handoff receipt 身份不完整")
	}
	key := receipt.TenantID + "\x00" + receipt.IdempotencyKey
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.items[key]; ok {
		return clone(existing), false, nil
	}
	idKey := receipt.TenantID + "\x00" + receipt.ID
	if existingKey, exists := s.ids[idKey]; exists && existingKey != key {
		return handoff.Receipt{}, false, fmt.Errorf("handoff receipt id %s 已存在", receipt.ID)
	}
	s.items[key] = clone(receipt)
	s.ids[idKey] = key
	return clone(receipt), true, nil
}

func clone(receipt handoff.Receipt) handoff.Receipt {
	receipt.Resources = append([]workmodel.ResourceRef(nil), receipt.Resources...)
	receipt.Artifacts = append([]artifactmodel.ArtifactRef(nil), receipt.Artifacts...)
	return receipt
}
