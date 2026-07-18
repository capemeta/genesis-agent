package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

// SessionExecutionBinding is the durable association between one immutable
// execution binding and the remote session workspace that executes it.
type SessionExecutionBinding struct {
	TenantID  string                       `json:"tenant_id"`
	RunID     string                       `json:"run_id"`
	BindingID string                       `json:"binding_id"`
	Workspace sandboxcontract.WorkspaceRef `json:"workspace"`
	ExpiresAt time.Time                    `json:"expires_at"`
}

func (b SessionExecutionBinding) validate() error {
	if b.TenantID != strings.TrimSpace(b.TenantID) || strings.TrimSpace(b.RunID) == "" || b.RunID != strings.TrimSpace(b.RunID) || strings.TrimSpace(b.BindingID) == "" || b.BindingID != strings.TrimSpace(b.BindingID) || strings.TrimSpace(b.Workspace.ID) == "" || b.ExpiresAt.IsZero() {
		return fmt.Errorf("session execution binding 无效")
	}
	if !reflect.DeepEqual(b.Workspace, cloneWorkspaceRef(b.Workspace)) {
		return fmt.Errorf("session execution binding workspace 包含非持久 metadata")
	}
	return nil
}

// BindSessionExecution is called by trusted execution Harness after opening a
// session and before running commands. Implementations must be exclusive or
// idempotent for an identical value.
type BindSessionExecution interface {
	BindSessionExecution(ctx context.Context, binding SessionExecutionBinding) error
}

// BindRemoteSession 为通用 Skill Harness 提供不暴露 adapter 模型的绑定入口。
func (s *FileSessionBindingStore) BindRemoteSession(ctx context.Context, tenantID, runID, bindingID string, workspace sandboxcontract.WorkspaceRef, expiresAt time.Time) error {
	return s.BindSessionExecution(ctx, SessionExecutionBinding{TenantID: tenantID, RunID: runID, BindingID: bindingID, Workspace: workspace, ExpiresAt: expiresAt})
}

type SessionBindingStore interface {
	BindSessionExecution
	GetSessionExecution(ctx context.Context, tenantID, runID, bindingID string) (SessionExecutionBinding, error)
}

// FileSessionBindingStore persists session bindings for a single-node product.
// Enterprise can inject a tenant database implementation of SessionBindingStore.
type FileSessionBindingStore struct {
	root string
	mu   sync.RWMutex
}

func NewFileSessionBindingStore(root string) (*FileSessionBindingStore, error) {
	abs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil || strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("session binding store root 无效")
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	return &FileSessionBindingStore{root: abs}, nil
}

func (s *FileSessionBindingStore) BindSessionExecution(ctx context.Context, binding SessionExecutionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	binding.Workspace = cloneWorkspaceRef(binding.Workspace)
	if err := binding.validate(); err != nil {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	data, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	filename := s.filename(binding.TenantID, binding.RunID, binding.BindingID)
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		existing, readErr := readSessionBinding(filename)
		if readErr != nil {
			return readErr
		}
		if equalSessionBinding(existing, binding) {
			return nil
		}
		// 同一 remote session 上允许单调延长 ExpiresAt（对齐 Session renew 滑动窗口）。
		if sameSessionWorkspace(existing, binding) && !binding.ExpiresAt.Before(existing.ExpiresAt) {
			return writeSessionBindingFile(filename, data)
		}
		return workcontract.NewError(workcontract.ErrCodeProducedResourceBackendMismatch, fmt.Errorf("execution binding 已绑定到不同 remote session"))
	}
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
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
	ok = true
	return nil
}

func writeSessionBindingFile(filename string, data []byte) error {
	tmp := filename + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	// Windows 上部分环境 Rename 不能覆盖已存在目标；先删再替换保证可移植。
	_ = os.Remove(filename)
	if err := os.Rename(tmp, filename); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *FileSessionBindingStore) GetSessionExecution(ctx context.Context, tenantID, runID, bindingID string) (SessionExecutionBinding, error) {
	if err := ctx.Err(); err != nil {
		return SessionExecutionBinding{}, err
	}
	s.mu.RLock()
	binding, err := readSessionBinding(s.filename(tenantID, runID, bindingID))
	s.mu.RUnlock()
	if os.IsNotExist(err) {
		return SessionExecutionBinding{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("execution binding 尚未绑定 remote session"))
	}
	if err != nil {
		return SessionExecutionBinding{}, err
	}
	if binding.TenantID != tenantID || binding.RunID != runID || binding.BindingID != bindingID {
		return SessionExecutionBinding{}, workcontract.NewError(workcontract.ErrCodeWorkspaceNotAvailable, fmt.Errorf("session binding scope 不匹配"))
	}
	return binding, nil
}

func readSessionBinding(filename string) (SessionExecutionBinding, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return SessionExecutionBinding{}, err
	}
	var binding SessionExecutionBinding
	if err := json.Unmarshal(data, &binding); err != nil || binding.validate() != nil {
		return SessionExecutionBinding{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, fmt.Errorf("session binding 已损坏"))
	}
	return binding, nil
}

func equalSessionBinding(left, right SessionExecutionBinding) bool {
	return left.TenantID == right.TenantID && left.RunID == right.RunID && left.BindingID == right.BindingID && left.ExpiresAt.Equal(right.ExpiresAt) && reflect.DeepEqual(left.Workspace, right.Workspace)
}

func sameSessionWorkspace(left, right SessionExecutionBinding) bool {
	return left.TenantID == right.TenantID && left.RunID == right.RunID && left.BindingID == right.BindingID && reflect.DeepEqual(left.Workspace, right.Workspace)
}

func (s *FileSessionBindingStore) filename(tenantID, runID, bindingID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(runID) + "\x00" + strings.TrimSpace(bindingID)))
	return filepath.Join(s.root, hex.EncodeToString(sum[:])+".json")
}
