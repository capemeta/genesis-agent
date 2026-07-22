package memory

import (
	"context"
	"fmt"
	"sync"

	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

type BindingStore struct {
	mu    sync.RWMutex
	byID  map[string]model.InvocationBinding
	byKey map[string]string
}

func NewBindingStore() *BindingStore {
	return &BindingStore{byID: map[string]model.InvocationBinding{}, byKey: map[string]string{}}
}

func (s *BindingStore) SaveBinding(_ context.Context, value model.InvocationBinding) (model.InvocationBinding, error) {
	if err := model.ValidateBindingIdentity(value); err != nil {
		return model.InvocationBinding{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id := s.byKey[value.IdempotencyKey]; id != "" {
		if id != value.ID {
			return model.InvocationBinding{}, fmt.Errorf("skill binding idempotency key 冲突: %s", value.IdempotencyKey)
		}
		return s.byID[id].Clone(), nil
	}
	if existing, ok := s.byID[value.ID]; ok && existing.IdempotencyKey != value.IdempotencyKey {
		return model.InvocationBinding{}, fmt.Errorf("skill binding id 冲突: %s", value.ID)
	}
	s.byID[value.ID] = value.Clone()
	s.byKey[value.IdempotencyKey] = value.ID
	return value.Clone(), nil
}

func (s *BindingStore) GetBinding(_ context.Context, id string) (model.InvocationBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.byID[id]
	if !ok {
		return model.InvocationBinding{}, contract.ErrInvocationBindingNotFound
	}
	return value.Clone(), nil
}

func (s *BindingStore) GetBindingByIdempotencyKey(_ context.Context, key string) (model.InvocationBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id := s.byKey[key]
	if id == "" {
		return model.InvocationBinding{}, contract.ErrInvocationBindingNotFound
	}
	return s.byID[id].Clone(), nil
}

func (s *BindingStore) ListBindingsByRun(_ context.Context, tenantID, runID string) ([]model.InvocationBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.InvocationBinding, 0)
	for _, value := range s.byID {
		if value.TenantID == tenantID && value.RunID == runID {
			out = append(out, value.Clone())
		}
	}
	return out, nil
}
