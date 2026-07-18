// Package subagent 提供 CLI 产品层的子智能体持久化适配。
package subagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

const storeSchemaVersion = 1

var safeAgentID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// FileStore 将控制面实例快照保存到工作区 .genesis/subagents。
type FileStore struct {
	dir string
	mu  sync.Mutex
}

type record struct {
	SchemaVersion int                     `json:"schema_version"`
	Stored        contract.StoredInstance `json:"stored"`
}

type deliveryRecord struct {
	SchemaVersion int                        `json:"schema_version"`
	Key           contract.ResultDeliveryKey `json:"key"`
	DeliveredAt   time.Time                  `json:"delivered_at"`
}

// StoredSummary 是 CLI 管理面展示用的实例摘要。
type StoredSummary struct {
	Stored  contract.StoredInstance
	ModTime time.Time
}

// CleanupOptions 控制文件 Store 的保守清理行为。
type CleanupOptions struct {
	OlderThan      time.Duration
	IncludeRunning bool
}

// CleanupResult 描述一次 best-effort 清理结果。
type CleanupResult struct {
	Deleted int
	Errors  int
}

// NewFileStore 创建 CLI 工作区级实例 Store。
func NewFileStore(workspaceRoot string) (*FileStore, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析工作区路径失败: %w", err)
	}
	dir := filepath.Join(filepath.Clean(abs), ".genesis", "runtime", "subagents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 subagent store 目录失败: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

func (s *FileStore) Save(ctx context.Context, value contract.StoredInstance) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	agentID := strings.TrimSpace(value.Instance.AgentID)
	if !safeAgentID.MatchString(agentID) {
		return fmt.Errorf("非法 subagent agent_id %q", agentID)
	}
	payload, err := json.MarshalIndent(record{SchemaVersion: storeSchemaVersion, Stored: value}, "", "  ")
	if err != nil {
		return fmt.Errorf("编码 subagent 实例失败: %w", err)
	}
	path := s.path(agentID)
	temp := path + ".tmp"
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.WriteFile(temp, payload, 0o600); err != nil {
		return fmt.Errorf("写入 subagent 实例临时文件失败: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("提交 subagent 实例文件失败: %w", err)
	}
	return nil
}

func (s *FileStore) Get(ctx context.Context, agentID string) (contract.StoredInstance, error) {
	if err := ctx.Err(); err != nil {
		return contract.StoredInstance{}, err
	}
	agentID = strings.TrimSpace(agentID)
	if !safeAgentID.MatchString(agentID) {
		return contract.StoredInstance{}, fmt.Errorf("非法 subagent agent_id %q", agentID)
	}
	raw, err := os.ReadFile(s.path(agentID))
	if err != nil {
		return contract.StoredInstance{}, fmt.Errorf("读取 subagent 实例失败: %w", err)
	}
	var stored record
	if err := json.Unmarshal(raw, &stored); err != nil {
		return contract.StoredInstance{}, fmt.Errorf("解析 subagent 实例失败: %w", err)
	}
	if stored.SchemaVersion != storeSchemaVersion {
		return contract.StoredInstance{}, fmt.Errorf("不支持的 subagent store schema_version=%d", stored.SchemaVersion)
	}
	if strings.TrimSpace(stored.Stored.Instance.AgentID) != agentID {
		return contract.StoredInstance{}, fmt.Errorf("subagent 实例文件与 agent_id 不一致")
	}
	return stored.Stored, nil
}

// List 返回当前工作区记录，按更新时间倒序排列。
func (s *FileStore) List(ctx context.Context) ([]StoredSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 subagent store 目录失败: %w", err)
	}
	out := make([]StoredSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		agentID := strings.TrimSuffix(entry.Name(), ".json")
		if !safeAgentID.MatchString(agentID) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("读取 subagent 实例文件信息失败: %w", err)
		}
		stored, err := s.Get(ctx, agentID)
		if err != nil {
			return nil, err
		}
		out = append(out, StoredSummary{Stored: stored, ModTime: info.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// Cleanup 删除早于阈值的终态记录；默认保留 running 记录。
func (s *FileStore) Cleanup(ctx context.Context, options CleanupOptions) (CleanupResult, error) {
	if err := ctx.Err(); err != nil {
		return CleanupResult{}, err
	}
	if options.OlderThan <= 0 {
		return CleanupResult{}, fmt.Errorf("cleanup OlderThan 必须大于 0")
	}
	items, err := s.List(ctx)
	if err != nil {
		return CleanupResult{}, err
	}
	cutoff := time.Now().Add(-options.OlderThan)
	var result CleanupResult
	for _, item := range items {
		instance := item.Stored.Instance
		if !options.IncludeRunning && instance.Status == model.StatusRunning {
			continue
		}
		if cleanupTime(instance, item.ModTime).After(cutoff) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		path := s.path(instance.AgentID)
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			result.Errors++
			continue
		}
		result.Deleted++
	}
	return result, nil
}

func (s *FileStore) MarkDelivered(ctx context.Context, key contract.ResultDeliveryKey) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	key = normalizeDeliveryKey(key)
	if err := validateDeliveryKey(key); err != nil {
		return false, err
	}
	payload, err := json.MarshalIndent(deliveryRecord{SchemaVersion: storeSchemaVersion, Key: key, DeliveredAt: time.Now()}, "", "  ")
	if err != nil {
		return false, fmt.Errorf("编码 TaskOutput 交付记录失败: %w", err)
	}
	dir := filepath.Join(s.dir, "deliveries")
	path := filepath.Join(dir, deliveryKeyHash(key)+".json")

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("创建 TaskOutput 交付目录失败: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("创建 TaskOutput 交付记录失败: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return false, fmt.Errorf("写入 TaskOutput 交付记录失败: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return false, fmt.Errorf("关闭 TaskOutput 交付记录失败: %w", err)
	}
	return false, nil
}

func (s *FileStore) path(agentID string) string {
	return filepath.Join(s.dir, agentID+".json")
}

func normalizeDeliveryKey(key contract.ResultDeliveryKey) contract.ResultDeliveryKey {
	key.TenantID = strings.TrimSpace(key.TenantID)
	key.SessionID = strings.TrimSpace(key.SessionID)
	key.ParentRunID = strings.TrimSpace(key.ParentRunID)
	key.AgentID = strings.TrimSpace(key.AgentID)
	key.ResultID = strings.TrimSpace(key.ResultID)
	return key
}

func validateDeliveryKey(key contract.ResultDeliveryKey) error {
	values := map[string]string{
		"tenant_id":     key.TenantID,
		"session_id":    key.SessionID,
		"parent_run_id": key.ParentRunID,
		"agent_id":      key.AgentID,
		"result_id":     key.ResultID,
	}
	for name, value := range values {
		if value == "" {
			return fmt.Errorf("TaskOutput 交付键缺少 %s", name)
		}
		if len(value) > 512 {
			return fmt.Errorf("TaskOutput 交付键 %s 过长", name)
		}
	}
	return nil
}

func deliveryKeyHash(key contract.ResultDeliveryKey) string {
	raw := key.TenantID + "\x00" + key.SessionID + "\x00" + key.ParentRunID + "\x00" + key.AgentID + "\x00" + key.ResultID
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func cleanupTime(instance model.Instance, fallback time.Time) time.Time {
	if instance.FinishedAt != nil && !instance.FinishedAt.IsZero() {
		return *instance.FinishedAt
	}
	if !instance.CreatedAt.IsZero() {
		return instance.CreatedAt
	}
	return fallback
}
