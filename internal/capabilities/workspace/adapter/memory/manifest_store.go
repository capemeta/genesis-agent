package memory

import (
	"context"
	"fmt"
	"sync"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ManifestStore 是无宿主路径依赖的并发安全测试实现。
type ManifestStore struct {
	mu    sync.RWMutex
	items map[string]workmodel.RunManifest
}

func NewManifestStore() *ManifestStore {
	return &ManifestStore{items: make(map[string]workmodel.RunManifest)}
}

func (s *ManifestStore) Create(ctx context.Context, manifest workmodel.RunManifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("Run manifest 无效: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := manifestKey(manifest.Scope.TenantID, manifest.RunID)
	if _, exists := s.items[key]; exists {
		return fmt.Errorf("Run manifest %s 已存在", manifest.RunID)
	}
	s.items[key] = cloneManifest(manifest)
	return nil
}

func (s *ManifestStore) Get(ctx context.Context, tenantID, runID string) (workmodel.RunManifest, error) {
	if err := ctx.Err(); err != nil {
		return workmodel.RunManifest{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	manifest, ok := s.items[manifestKey(tenantID, runID)]
	if !ok {
		return workmodel.RunManifest{}, fmt.Errorf("Run manifest %s 不存在", runID)
	}
	return cloneManifest(manifest), nil
}

func (s *ManifestStore) AddExecution(ctx context.Context, tenantID, runID string, expectedRevision uint64, execution workmodel.PreparedExecutionSnapshot) (workmodel.RunManifest, error) {
	if err := ctx.Err(); err != nil {
		return workmodel.RunManifest{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := manifestKey(tenantID, runID)
	manifest, ok := s.items[key]
	if !ok {
		return workmodel.RunManifest{}, fmt.Errorf("Run manifest %s 不存在", runID)
	}
	if manifest.Revision != expectedRevision {
		return workmodel.RunManifest{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("Run manifest revision 冲突: expected=%d actual=%d", expectedRevision, manifest.Revision))
	}
	for _, existing := range manifest.Executions {
		if existing.Binding.ID == execution.Binding.ID {
			return workmodel.RunManifest{}, fmt.Errorf("execution binding %s 已存在", execution.Binding.ID)
		}
		if existing.Binding.Owner.SameExecutionSubject(execution.Binding.Owner) {
			return workmodel.RunManifest{}, fmt.Errorf("execution subject 已存在")
		}
	}
	manifest.Executions = append(manifest.Executions, execution)
	manifest.Revision++
	if err := manifest.Validate(); err != nil {
		return workmodel.RunManifest{}, err
	}
	s.items[key] = cloneManifest(manifest)
	return cloneManifest(manifest), nil
}

func manifestKey(tenantID, runID string) string { return tenantID + "\x00" + runID }

func cloneManifest(manifest workmodel.RunManifest) workmodel.RunManifest {
	manifest.AgentApp.Workspace.AllowedModes = append([]execmodel.WorkspaceMode(nil), manifest.AgentApp.Workspace.AllowedModes...)
	manifest.Limits.ProductModes = append([]execmodel.WorkspaceMode(nil), manifest.Limits.ProductModes...)
	manifest.Limits.PolicyModes = append([]execmodel.WorkspaceMode(nil), manifest.Limits.PolicyModes...)
	manifest.Limits.BackendModes = append([]execmodel.WorkspaceMode(nil), manifest.Limits.BackendModes...)
	if manifest.ProjectRoot != nil {
		copied := *manifest.ProjectRoot
		manifest.ProjectRoot = &copied
	}
	manifest.Executions = append([]workmodel.PreparedExecutionSnapshot(nil), manifest.Executions...)
	for i := range manifest.Executions {
		metadata := manifest.Executions[i].Workspace.Metadata
		if metadata == nil {
			continue
		}
		manifest.Executions[i].Workspace.Metadata = make(map[string]string, len(metadata))
		for key, value := range metadata {
			manifest.Executions[i].Workspace.Metadata[key] = value
		}
	}
	return manifest
}
