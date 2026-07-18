package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ManifestStore 把本地 Run manifest 排他写入显式 state root。
type ManifestStore struct {
	stateRoot string
	mu        sync.RWMutex
}

func NewManifestStore(stateRoot string) (*ManifestStore, error) {
	root, err := filepath.Abs(strings.TrimSpace(stateRoot))
	if err != nil || strings.TrimSpace(stateRoot) == "" {
		return nil, workcontract.NewError(workcontract.ErrCodeStateRootUnavailable, fmt.Errorf("manifest store 缺少有效 state root"))
	}
	return &ManifestStore{stateRoot: root}, nil
}

func (s *ManifestStore) Create(ctx context.Context, manifest workmodel.RunManifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("Run manifest 不完整: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("编码 Run manifest: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	filename := s.revisionFilename(manifest.Scope.TenantID, manifest.RunID, manifest.Revision)
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return fmt.Errorf("创建 Run manifest 目录: %w", err)
	}
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("排他创建 Run manifest: %w", err)
	}
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(filename)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("写入 Run manifest: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("同步 Run manifest: %w", err)
	}
	committed = true
	_ = os.WriteFile(s.filename(manifest.Scope.TenantID, manifest.RunID), data, 0o600)
	return nil
}

func (s *ManifestStore) Get(ctx context.Context, tenantID, runID string) (workmodel.RunManifest, error) {
	if err := ctx.Err(); err != nil {
		return workmodel.RunManifest{}, err
	}
	s.mu.RLock()
	data, err := s.readLatest(tenantID, runID)
	s.mu.RUnlock()
	if err != nil {
		return workmodel.RunManifest{}, fmt.Errorf("读取 Run manifest: %w", err)
	}
	var manifest workmodel.RunManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return workmodel.RunManifest{}, fmt.Errorf("解析 Run manifest: %w", err)
	}
	if manifest.Scope.TenantID != strings.TrimSpace(tenantID) {
		return workmodel.RunManifest{}, fmt.Errorf("Run manifest tenant scope 不匹配")
	}
	if err := manifest.Validate(); err != nil {
		return workmodel.RunManifest{}, fmt.Errorf("Run manifest 无效: %w", err)
	}
	return manifest, nil
}

func (s *ManifestStore) AddExecution(ctx context.Context, tenantID, runID string, expectedRevision uint64, execution workmodel.PreparedExecutionSnapshot) (workmodel.RunManifest, error) {
	if err := ctx.Err(); err != nil {
		return workmodel.RunManifest{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readLatest(tenantID, runID)
	if err != nil {
		return workmodel.RunManifest{}, err
	}
	var manifest workmodel.RunManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return workmodel.RunManifest{}, fmt.Errorf("解析 Run manifest: %w", err)
	}
	if manifest.Scope.TenantID != strings.TrimSpace(tenantID) {
		return workmodel.RunManifest{}, fmt.Errorf("Run manifest tenant scope 不匹配")
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
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return workmodel.RunManifest{}, err
	}
	filename := s.revisionFilename(tenantID, runID, manifest.Revision)
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return workmodel.RunManifest{}, fmt.Errorf("创建 manifest revision: %w", err)
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(filename)
		return workmodel.RunManifest{}, err
	}
	if closeErr != nil {
		_ = os.Remove(filename)
		return workmodel.RunManifest{}, closeErr
	}
	_ = os.WriteFile(s.filename(tenantID, runID), data, 0o600)
	return manifest, nil
}

func (s *ManifestStore) filename(tenantID, runID string) string {
	return filepath.Join(s.stateRoot, "runtime", "runs", storageKey(tenantID), storageKey(runID), "manifest.json")
}

func (s *ManifestStore) revisionFilename(tenantID, runID string, revision uint64) string {
	return filepath.Join(s.stateRoot, "runtime", "runs", storageKey(tenantID), storageKey(runID), "manifest."+strconv.FormatUint(revision, 10)+".json")
}

func storageKey(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func (s *ManifestStore) readLatest(tenantID, runID string) ([]byte, error) {
	dir := filepath.Dir(s.filename(tenantID, runID))
	entries, err := filepath.Glob(filepath.Join(dir, "manifest.*.json"))
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return manifestRevision(entries[i]) > manifestRevision(entries[j]) })
	for _, entry := range entries {
		data, readErr := os.ReadFile(entry)
		if readErr != nil {
			continue
		}
		var manifest workmodel.RunManifest
		if json.Unmarshal(data, &manifest) == nil && manifest.Validate() == nil {
			return data, nil
		}
	}
	return os.ReadFile(s.filename(tenantID, runID))
}

func manifestRevision(filename string) uint64 {
	base := filepath.Base(filename)
	value := strings.TrimSuffix(strings.TrimPrefix(base, "manifest."), ".json")
	revision, _ := strconv.ParseUint(value, 10, 64)
	return revision
}
