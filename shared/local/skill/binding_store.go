package skill

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
)

// BindingStore 是 InvocationBinding 的宿主持久化适配器。每个 binding 单文件、原子提交，
// 内存索引只做加速；磁盘记录是恢复真相源。
type BindingStore struct {
	dir   string
	mu    sync.RWMutex
	byID  map[string]skillmodel.InvocationBinding
	byKey map[string]string
}

func NewBindingStore(stateRoot string) (*BindingStore, error) {
	stateRoot = strings.TrimSpace(stateRoot)
	if stateRoot == "" {
		return nil, fmt.Errorf("skill binding state root 不能为空")
	}
	root, err := filepath.Abs(stateRoot)
	if err != nil || root == "" {
		return nil, fmt.Errorf("解析 skill binding state root: %w", err)
	}
	dir := filepath.Join(root, "runtime", "skill-bindings")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 skill binding 目录: %w", err)
	}
	s := &BindingStore{dir: dir, byID: map[string]skillmodel.InvocationBinding{}, byKey: map[string]string{}}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *BindingStore) reload() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("扫描 skill binding: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if readErr != nil {
			return fmt.Errorf("读取 skill binding %s: %w", entry.Name(), readErr)
		}
		var value skillmodel.InvocationBinding
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("解析 skill binding %s: %w", entry.Name(), err)
		}
		if err := skillmodel.ValidateBindingIdentity(value); err != nil {
			return fmt.Errorf("校验 skill binding %s: %w", entry.Name(), err)
		}
		if prior := s.byKey[value.IdempotencyKey]; prior != "" && prior != value.ID {
			return fmt.Errorf("skill binding idempotency key 冲突: %s", value.IdempotencyKey)
		}
		if prior, ok := s.byID[value.ID]; ok && prior.IdempotencyKey != value.IdempotencyKey {
			return fmt.Errorf("skill binding id 冲突: %s", value.ID)
		}
		s.byID[value.ID] = value.Clone()
		s.byKey[value.IdempotencyKey] = value.ID
	}
	return nil
}

func (s *BindingStore) SaveBinding(_ context.Context, value skillmodel.InvocationBinding) (skillmodel.InvocationBinding, error) {
	if err := skillmodel.ValidateBindingIdentity(value); err != nil {
		return skillmodel.InvocationBinding{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id := s.byKey[value.IdempotencyKey]; id != "" {
		if id != value.ID {
			return skillmodel.InvocationBinding{}, fmt.Errorf("skill binding idempotency key 冲突: %s", value.IdempotencyKey)
		}
		return s.byID[id].Clone(), nil
	}
	if existing, ok := s.byID[value.ID]; ok && existing.IdempotencyKey != value.IdempotencyKey {
		return skillmodel.InvocationBinding{}, fmt.Errorf("skill binding id 冲突: %s", value.ID)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return skillmodel.InvocationBinding{}, fmt.Errorf("编码 skill binding: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, ".binding-*.tmp")
	if err != nil {
		return skillmodel.InvocationBinding{}, fmt.Errorf("创建 skill binding 临时文件: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return skillmodel.InvocationBinding{}, fmt.Errorf("写入 skill binding: %w", err)
	}
	// ID 是运行时标识而不是可信文件名。使用固定长度摘要可避免路径分隔符、保留名
	// 和超长 ID 越过 state root，同时 reload 仍以文件内的 binding 为真相源。
	finalName := filepath.Join(s.dir, bindingFileName(value.ID))
	if err := os.Rename(tmpName, finalName); err != nil {
		cleanup()
		if existing, ok := s.byID[value.ID]; ok && existing.IdempotencyKey == value.IdempotencyKey {
			return existing.Clone(), nil
		}
		return skillmodel.InvocationBinding{}, fmt.Errorf("提交 skill binding: %w", err)
	}
	s.byID[value.ID] = value.Clone()
	s.byKey[value.IdempotencyKey] = value.ID
	return value.Clone(), nil
}

func bindingFileName(id string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(id)))
	return fmt.Sprintf("%x.json", digest[:])
}

func (s *BindingStore) GetBinding(_ context.Context, id string) (skillmodel.InvocationBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.byID[strings.TrimSpace(id)]
	if !ok {
		return skillmodel.InvocationBinding{}, skillcontract.ErrInvocationBindingNotFound
	}
	return value.Clone(), nil
}

func (s *BindingStore) GetBindingByIdempotencyKey(_ context.Context, key string) (skillmodel.InvocationBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id := s.byKey[strings.TrimSpace(key)]
	if id == "" {
		return skillmodel.InvocationBinding{}, skillcontract.ErrInvocationBindingNotFound
	}
	return s.byID[id].Clone(), nil
}

func (s *BindingStore) ListBindingsByRun(_ context.Context, tenantID, runID string) ([]skillmodel.InvocationBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]skillmodel.InvocationBinding, 0)
	for _, value := range s.byID {
		if value.TenantID == tenantID && value.RunID == runID {
			out = append(out, value.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
