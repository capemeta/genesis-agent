package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ProducedResourceStore is a concurrent in-memory implementation for tests and ephemeral products.
type ProducedResourceStore struct {
	mu      sync.RWMutex
	byID    map[string]workmodel.ProducedResourceDescriptor
	logical map[string]string
}

func NewProducedResourceStore() *ProducedResourceStore {
	return &ProducedResourceStore{byID: make(map[string]workmodel.ProducedResourceDescriptor), logical: make(map[string]string)}
}

func (s *ProducedResourceStore) Create(ctx context.Context, descriptor workmodel.ProducedResourceDescriptor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := descriptor.Validate(); err != nil {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idKey := producedIDKey(descriptor.TenantID, descriptor.RunID, descriptor.ID)
	logicalKey := producedLogicalKey(descriptor.TenantID, descriptor.RunID, descriptor.LogicalRef)
	if _, exists := s.byID[idKey]; exists {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceConflict, fmt.Errorf("produced resource id %s 已存在", descriptor.ID))
	}
	if _, exists := s.logical[logicalKey]; exists {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceConflict, fmt.Errorf("produced resource logical_ref %s 已存在", descriptor.LogicalRef))
	}
	s.byID[idKey] = cloneProducedResource(descriptor)
	s.logical[logicalKey] = descriptor.ID
	return nil
}

func (s *ProducedResourceStore) UpsertCurrent(ctx context.Context, descriptor workmodel.ProducedResourceDescriptor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := descriptor.Validate(); err != nil {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idKey := producedIDKey(descriptor.TenantID, descriptor.RunID, descriptor.ID)
	logicalKey := producedLogicalKey(descriptor.TenantID, descriptor.RunID, descriptor.LogicalRef)
	if _, exists := s.byID[idKey]; exists {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceConflict, fmt.Errorf("produced resource id %s 已存在", descriptor.ID))
	}
	s.byID[idKey] = cloneProducedResource(descriptor)
	s.logical[logicalKey] = descriptor.ID
	return nil
}

func (s *ProducedResourceStore) Get(ctx context.Context, tenantID, runID, producedResourceID string) (workmodel.ProducedResourceDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return workmodel.ProducedResourceDescriptor{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.byID[producedIDKey(tenantID, runID, producedResourceID)]
	if !ok {
		return workmodel.ProducedResourceDescriptor{}, workcontract.NewError(workcontract.ErrCodeProducedResourceNotFound, fmt.Errorf("produced resource %s 不存在", producedResourceID))
	}
	return cloneProducedResource(value), nil
}

func (s *ProducedResourceStore) GetByLogicalRef(ctx context.Context, tenantID, runID, logicalRef string) (workmodel.ProducedResourceDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return workmodel.ProducedResourceDescriptor{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.logical[producedLogicalKey(tenantID, runID, logicalRef)]
	if !ok {
		return workmodel.ProducedResourceDescriptor{}, workcontract.NewError(workcontract.ErrCodeProducedResourceNotFound, fmt.Errorf("produced resource logical_ref %s 不存在", logicalRef))
	}
	return cloneProducedResource(s.byID[producedIDKey(tenantID, runID, id)]), nil
}

func (s *ProducedResourceStore) ListByRun(ctx context.Context, tenantID, runID string) ([]workmodel.ProducedResourceDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := tenantID + "\x00" + runID + "\x00"
	result := make([]workmodel.ProducedResourceDescriptor, 0)
	for logicalKey, id := range s.logical {
		if !strings.HasPrefix(logicalKey, prefix) {
			continue
		}
		value, ok := s.byID[producedIDKey(tenantID, runID, id)]
		if !ok {
			continue
		}
		result = append(result, cloneProducedResource(value))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

func producedIDKey(tenantID, runID, id string) string { return tenantID + "\x00" + runID + "\x00" + id }
func producedLogicalKey(tenantID, runID, logical string) string {
	return tenantID + "\x00" + runID + "\x00" + logical
}

func cloneProducedResource(value workmodel.ProducedResourceDescriptor) workmodel.ProducedResourceDescriptor {
	if value.ExpiresAt != nil {
		cloned := *value.ExpiresAt
		value.ExpiresAt = &cloned
	}
	return value
}
