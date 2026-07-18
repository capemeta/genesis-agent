package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

const (
	RemoteExecutorAuthority = "remote-executor"
	SessionFileScheme       = "session-file"
	ExecutorObjectScheme    = "executor-object"
)

// RemoteLocator is persisted control-plane data. It never contains credentials,
// clients or a live sandbox session.
type RemoteLocator struct {
	ID           string                         `json:"id"`
	Authority    string                         `json:"authority"`
	Scheme       string                         `json:"scheme"`
	Workspace    sandboxcontract.WorkspaceRef   `json:"workspace,omitempty"`
	Path         workmodel.WorkspacePath        `json:"path,omitempty"`
	ObjectID     string                         `json:"object_id,omitempty"`
	Scope        workmodel.ResourceScope        `json:"scope"`
	Version      string                         `json:"version"`
	MediaType    string                         `json:"media_type,omitempty"`
	Size         int64                          `json:"size"`
	Availability workmodel.ResourceAvailability `json:"availability"`
	ExpiresAt    *time.Time                     `json:"expires_at,omitempty"`
}

func (l RemoteLocator) validate() error {
	if strings.TrimSpace(l.ID) == "" || l.ID != strings.TrimSpace(l.ID) || l.Authority != RemoteExecutorAuthority {
		return fmt.Errorf("remote locator identity 无效")
	}
	if strings.TrimSpace(l.Version) == "" || l.Version != strings.TrimSpace(l.Version) || l.Size < 0 {
		return fmt.Errorf("remote locator version/size 无效")
	}
	switch l.Scheme {
	case SessionFileScheme:
		if l.Availability != workmodel.ResourceAvailabilityLeased || l.ExpiresAt == nil || l.ExpiresAt.IsZero() || strings.TrimSpace(l.Workspace.ID) == "" || l.Workspace.ID != strings.TrimSpace(l.Workspace.ID) || l.Workspace.Provider != strings.TrimSpace(l.Workspace.Provider) || l.Path.Validate() != nil || l.ObjectID != "" {
			return fmt.Errorf("session-file locator 无效")
		}
		for key, value := range l.Workspace.Metadata {
			if key != "session_id" && key != "workspace_id" && key != "sandbox_id" || strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
				return fmt.Errorf("session-file locator metadata 无效")
			}
		}
	case ExecutorObjectScheme:
		if l.Availability != workmodel.ResourceAvailabilityDurable || strings.TrimSpace(l.ObjectID) == "" || l.ObjectID != strings.TrimSpace(l.ObjectID) || l.Path != "" || strings.TrimSpace(l.Workspace.ID) != "" {
			return fmt.Errorf("executor-object locator 无效")
		}
	default:
		return fmt.Errorf("remote locator scheme 无效: %q", l.Scheme)
	}
	return nil
}

// RemoteLocatorStore must persist locators with exclusive Create semantics.
// Put 仅用于刷新已存在 locator 的元数据（如 leased ExpiresAt），不得改写 Version/Path 身份字段以外的内容指纹。
// Enterprise may provide a tenant database implementation; FileRemoteLocatorStore
// is suitable for single-node products and tests.
type RemoteLocatorStore interface {
	Create(ctx context.Context, locator RemoteLocator) error
	Put(ctx context.Context, locator RemoteLocator) error
	Get(ctx context.Context, id string, scope workmodel.ResourceScope) (RemoteLocator, error)
}

type FileRemoteLocatorStore struct {
	root string
	mu   sync.RWMutex
}

func NewFileRemoteLocatorStore(root string) (*FileRemoteLocatorStore, error) {
	abs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil || strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("remote locator store root 无效")
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	return &FileRemoteLocatorStore{root: abs}, nil
}

func (s *FileRemoteLocatorStore) Create(ctx context.Context, locator RemoteLocator) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := locator.validate(); err != nil {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	data, err := json.Marshal(locator)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(s.filename(locator.ID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceConflict, fmt.Errorf("remote locator %s 已存在", locator.ID))
	}
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(file.Name())
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

func (s *FileRemoteLocatorStore) Put(ctx context.Context, locator RemoteLocator) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := locator.validate(); err != nil {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	data, err := json.Marshal(locator)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	filename := s.filename(locator.ID)
	if _, err := os.Stat(filename); err != nil {
		if os.IsNotExist(err) {
			return workcontract.NewError(workcontract.ErrCodeProducedResourceNotFound, fmt.Errorf("remote locator %s 不存在", locator.ID))
		}
		return err
	}
	tmp := filename + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filename); err != nil {
		_ = os.Remove(tmp)
		// Windows：目标已存在时 Rename 可能失败，回退为直接覆盖写入。
		if writeErr := os.WriteFile(filename, data, 0o600); writeErr != nil {
			return err
		}
	}
	return nil
}

func (s *FileRemoteLocatorStore) Get(ctx context.Context, id string, scope workmodel.ResourceScope) (RemoteLocator, error) {
	if err := ctx.Err(); err != nil {
		return RemoteLocator{}, err
	}
	s.mu.RLock()
	data, err := os.ReadFile(s.filename(id))
	s.mu.RUnlock()
	if os.IsNotExist(err) {
		return RemoteLocator{}, workcontract.NewError(workcontract.ErrCodeProducedResourceNotFound, fmt.Errorf("remote locator 不存在"))
	}
	if err != nil {
		return RemoteLocator{}, err
	}
	var locator RemoteLocator
	if err := json.Unmarshal(data, &locator); err != nil || locator.validate() != nil {
		return RemoteLocator{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, fmt.Errorf("remote locator 已损坏"))
	}
	if locator.ID != id || locator.Scope != scope {
		return RemoteLocator{}, workcontract.NewError(workcontract.ErrCodeProducedResourceNotFound, fmt.Errorf("remote locator 不存在"))
	}
	return locator, nil
}

func (s *FileRemoteLocatorStore) filename(id string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(id)))
	return filepath.Join(s.root, hex.EncodeToString(sum[:])+".json")
}

type LocatorIDGenerator interface{ Generate() string }

func validateRemoteBackend(backend execmodel.ExecutionBackendRef) error {
	if err := backend.Validate(); err != nil {
		return err
	}
	if backend.Kind != execmodel.BackendKindRemote || backend.Authority != RemoteExecutorAuthority {
		return fmt.Errorf("execution backend 不是 remote-executor")
	}
	return nil
}
