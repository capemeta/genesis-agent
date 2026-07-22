package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ProducedResourceStore persists immutable descriptors below the local control root.
type ProducedResourceStore struct {
	stateRoot string
	mu        sync.RWMutex
}

func NewProducedResourceStore(stateRoot string) (*ProducedResourceStore, error) {
	root, err := localStateRoot(stateRoot)
	if err != nil {
		return nil, err
	}
	return &ProducedResourceStore{stateRoot: root}, nil
}

func (s *ProducedResourceStore) Create(ctx context.Context, descriptor workmodel.ProducedResourceDescriptor) error {
	return s.persist(ctx, descriptor, false)
}

func (s *ProducedResourceStore) UpsertCurrent(ctx context.Context, descriptor workmodel.ProducedResourceDescriptor) error {
	return s.persist(ctx, descriptor, true)
}

func (s *ProducedResourceStore) persist(ctx context.Context, descriptor workmodel.ProducedResourceDescriptor, replaceLogical bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := descriptor.Validate(); err != nil {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	if descriptor.Source.Path != "" {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, fmt.Errorf("produced descriptor 禁止持久化物理 source path"))
	}
	data, err := json.MarshalIndent(descriptor, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.runDir(descriptor.TenantID, descriptor.RunID)
	if err := os.MkdirAll(filepath.Join(dir, "by-id"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "by-logical"), 0o700); err != nil {
		return err
	}
	filename := s.idFilename(descriptor.TenantID, descriptor.RunID, descriptor.ID)
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return workcontract.NewError(workcontract.ErrCodeProducedResourceConflict, fmt.Errorf("produced resource id %s 已存在", descriptor.ID))
		}
		return err
	}
	idCommitted := false
	defer func() {
		_ = file.Close()
		if !idCommitted {
			_ = os.Remove(filename)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	logicalFile := s.logicalFilename(descriptor.TenantID, descriptor.RunID, descriptor.LogicalRef)
	if replaceLogical {
		// 已持 store 锁；短 ID 写入足够保证同进程互斥，历史 by-id 仍保留。
		if err := os.WriteFile(logicalFile, []byte(descriptor.ID), 0o600); err != nil {
			return err
		}
	} else {
		lock, err := os.OpenFile(logicalFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if os.IsExist(err) {
				return workcontract.NewError(workcontract.ErrCodeProducedResourceConflict, fmt.Errorf("produced logical_ref 已存在"))
			}
			return err
		}
		if _, err := lock.WriteString(descriptor.ID); err != nil {
			_ = lock.Close()
			_ = os.Remove(logicalFile)
			return err
		}
		if err := lock.Sync(); err != nil {
			_ = lock.Close()
			_ = os.Remove(logicalFile)
			return err
		}
		if err := lock.Close(); err != nil {
			_ = os.Remove(logicalFile)
			return err
		}
	}
	idCommitted = true
	return nil
}

func (s *ProducedResourceStore) Get(ctx context.Context, tenantID, runID, producedResourceID string) (workmodel.ProducedResourceDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return workmodel.ProducedResourceDescriptor{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	// ProducedResourceStore 只做本 Run scope 的同 Run 查找；跨 Run 可读性由消费者（如 view_image / finalizer）
	// 依 AdoptionRecord 显式解析所属 Run 后再按 owner scope 读取，store 层不做任何隐式跨 Run 回退。
	return readProducedDescriptor(s.idFilename(tenantID, runID, producedResourceID), tenantID, runID)
}

func (s *ProducedResourceStore) GetByLogicalRef(ctx context.Context, tenantID, runID, logicalRef string) (workmodel.ProducedResourceDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return workmodel.ProducedResourceDescriptor{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, err := os.ReadFile(s.logicalFilename(tenantID, runID, logicalRef))
	if err != nil {
		return workmodel.ProducedResourceDescriptor{}, producedNotFound(err)
	}
	return readProducedDescriptor(s.idFilename(tenantID, runID, string(id)), tenantID, runID)
}

func (s *ProducedResourceStore) ListByRun(ctx context.Context, tenantID, runID string) ([]workmodel.ProducedResourceDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	refs, err := filepath.Glob(filepath.Join(s.runDir(tenantID, runID), "by-logical", "*.ref"))
	if err != nil {
		return nil, err
	}
	result := make([]workmodel.ProducedResourceDescriptor, 0, len(refs))
	for _, refFile := range refs {
		id, readErr := os.ReadFile(refFile)
		if readErr != nil {
			return nil, producedNotFound(readErr)
		}
		descriptor, readErr := readProducedDescriptor(s.idFilename(tenantID, runID, string(id)), tenantID, runID)
		if readErr != nil {
			return nil, readErr
		}
		result = append(result, descriptor)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

func (s *ProducedResourceStore) runDir(tenantID, runID string) string {
	return filepath.Join(s.stateRoot, "runtime", "runs", storageKey(tenantID), storageKey(runID), "produced")
}

func (s *ProducedResourceStore) idFilename(tenantID, runID, id string) string {
	return filepath.Join(s.runDir(tenantID, runID), "by-id", storageKey(id)+".json")
}

func (s *ProducedResourceStore) logicalFilename(tenantID, runID, logical string) string {
	return filepath.Join(s.runDir(tenantID, runID), "by-logical", storageKey(logical)+".ref")
}

func readProducedDescriptor(filename, tenantID, runID string) (workmodel.ProducedResourceDescriptor, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return workmodel.ProducedResourceDescriptor{}, producedNotFound(err)
	}
	var descriptor workmodel.ProducedResourceDescriptor
	if err := json.Unmarshal(data, &descriptor); err != nil {
		return workmodel.ProducedResourceDescriptor{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, fmt.Errorf("解析 produced resource: %w", err))
	}
	if expectedTenant := strings.TrimSpace(tenantID); expectedTenant != "" && descriptor.TenantID != expectedTenant {
		return workmodel.ProducedResourceDescriptor{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("produced resource scope 不匹配"))
	}
	if err := descriptor.Validate(); err != nil {
		return workmodel.ProducedResourceDescriptor{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	return descriptor, nil
}

func producedNotFound(err error) error {
	if os.IsNotExist(err) {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceNotFound, err)
	}
	return err
}

func localStateRoot(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", workcontract.NewError(workcontract.ErrCodeStateRootUnavailable, fmt.Errorf("local store 缺少 state root"))
	}
	root, err := filepath.Abs(value)
	if err != nil {
		return "", workcontract.NewError(workcontract.ErrCodeStateRootUnavailable, err)
	}
	return root, nil
}
