// Package subagent 提供单节点产品可持久恢复的子 Run 实例存储。
package subagent

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

	"genesis-agent/internal/runtime/multiagent/contract"
)

const schemaVersion = 1

type record struct {
	SchemaVersion int                     `json:"schema_version"`
	Stored        contract.StoredInstance `json:"stored"`
}

// Store 使用实例快照和 O_EXCL invocation claim 实现进程重启后的单次派生语义。
type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(stateRoot string) (*Store, error) {
	stateRoot = strings.TrimSpace(stateRoot)
	if stateRoot == "" {
		return nil, fmt.Errorf("subagent state root不能为空")
	}
	root, err := filepath.Abs(stateRoot)
	if err != nil || root == "" {
		return nil, fmt.Errorf("解析 subagent state root: %w", err)
	}
	dir := filepath.Join(root, "runtime", "subagents")
	if err := os.MkdirAll(filepath.Join(dir, "invocations"), 0o700); err != nil {
		return nil, fmt.Errorf("创建 subagent store: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Save(ctx context.Context, value contract.StoredInstance) error {
	if err := validateStoredInstance(value); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(value)
}

func (s *Store) SaveIfInvocationAbsent(ctx context.Context, value contract.StoredInstance) (contract.StoredInstance, bool, error) {
	if err := validateStoredInstance(value); err != nil {
		return contract.StoredInstance{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return contract.StoredInstance{}, false, err
	}
	if strings.TrimSpace(value.Request.InvocationBinding.ID) == "" {
		return value, true, s.Save(ctx, value)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	claimPath := filepath.Join(s.dir, "invocations", invocationKey(value)+".json")
	payload, err := json.Marshal(record{SchemaVersion: schemaVersion, Stored: value})
	if err != nil {
		return contract.StoredInstance{}, false, err
	}
	claim, err := os.OpenFile(claimPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if !os.IsExist(err) {
			return contract.StoredInstance{}, false, fmt.Errorf("创建 subagent invocation claim: %w", err)
		}
		existing, err := s.readClaimLocked(claimPath)
		if err != nil {
			return contract.StoredInstance{}, false, err
		}
		if invocationKey(existing) != invocationKey(value) {
			return contract.StoredInstance{}, false, fmt.Errorf("subagent invocation claim身份冲突")
		}
		if latest, err := s.getLocked(existing.Instance.AgentID); err == nil {
			existing = latest
		}
		return existing, false, nil
	}
	if _, err := claim.Write(payload); err != nil {
		_ = claim.Close()
		_ = os.Remove(claimPath)
		return contract.StoredInstance{}, false, fmt.Errorf("写入 subagent invocation claim: %w", err)
	}
	if err := claim.Sync(); err == nil {
		err = claim.Close()
	} else {
		_ = claim.Close()
	}
	if err != nil {
		_ = os.Remove(claimPath)
		return contract.StoredInstance{}, false, err
	}
	if err := s.saveLocked(value); err != nil {
		_ = os.Remove(claimPath)
		return contract.StoredInstance{}, false, err
	}
	return value, true, nil
}

func (s *Store) Get(ctx context.Context, agentID string) (contract.StoredInstance, error) {
	if err := ctx.Err(); err != nil {
		return contract.StoredInstance{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(strings.TrimSpace(agentID))
}

func (s *Store) saveLocked(value contract.StoredInstance) error {
	payload, err := json.Marshal(record{SchemaVersion: schemaVersion, Stored: value})
	if err != nil {
		return fmt.Errorf("编码 subagent instance: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, ".instance-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(payload)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return err
	}
	final := s.instancePath(value.Instance.AgentID)
	if err := os.Rename(tmpName, final); err != nil {
		cleanup()
		return fmt.Errorf("提交 subagent instance: %w", err)
	}
	return nil
}

func (s *Store) getLocked(agentID string) (contract.StoredInstance, error) {
	if agentID == "" {
		return contract.StoredInstance{}, fmt.Errorf("subagent agent_id不能为空")
	}
	raw, err := os.ReadFile(s.instancePath(agentID))
	if err != nil {
		return contract.StoredInstance{}, fmt.Errorf("读取 subagent instance: %w", err)
	}
	var value record
	if err := json.Unmarshal(raw, &value); err != nil || value.SchemaVersion != schemaVersion {
		return contract.StoredInstance{}, fmt.Errorf("解析 subagent instance失败")
	}
	if value.Stored.Instance.AgentID != agentID {
		return contract.StoredInstance{}, fmt.Errorf("subagent instance身份不一致")
	}
	return value.Stored, nil
}

func (s *Store) readClaimLocked(path string) (contract.StoredInstance, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return contract.StoredInstance{}, err
	}
	var value record
	if err := json.Unmarshal(raw, &value); err != nil || value.SchemaVersion != schemaVersion || strings.TrimSpace(value.Stored.Request.InvocationBinding.ID) == "" {
		return contract.StoredInstance{}, fmt.Errorf("解析 subagent invocation claim失败")
	}
	return value.Stored, nil
}

func (s *Store) instancePath(agentID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(agentID)))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

func invocationKey(value contract.StoredInstance) string {
	raw := strings.TrimSpace(value.Request.TenantID) + "\x00" + strings.TrimSpace(value.Request.ParentRunID) + "\x00" + strings.TrimSpace(value.Request.InvocationBinding.ID)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func validateStoredInstance(value contract.StoredInstance) error {
	if strings.TrimSpace(value.Instance.AgentID) == "" || strings.TrimSpace(value.Instance.ParentRunID) == "" || strings.TrimSpace(value.Instance.TenantID) == "" {
		return fmt.Errorf("subagent instance缺少 agent_id/parent_run_id/tenant_id")
	}
	if value.Request.ParentRunID != value.Instance.ParentRunID || value.Request.TenantID != value.Instance.TenantID {
		return fmt.Errorf("subagent instance与spawn request身份不一致")
	}
	return nil
}

var _ contract.InstanceStore = (*Store)(nil)
