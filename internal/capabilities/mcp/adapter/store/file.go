package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"genesis-agent/internal/capabilities/mcp/contract"
)

// FileApprovalStore 将 project MCP 预连接审批持久化到 JSON 文件。
type FileApprovalStore struct {
	path string
	mu   sync.Mutex
}

// NewFile 创建文件审批存储；目录不存在时在首次 Put 时创建。
func NewFile(path string) (*FileApprovalStore, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, fmt.Errorf("mcp approval store path 不能为空")
	}
	return &FileApprovalStore{path: path}, nil
}

func (s *FileApprovalStore) Get(ctx context.Context, serverName string) (contract.ApprovalDecision, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.read()
	if err != nil {
		return "", false, err
	}
	v, ok := items[serverName]
	return v, ok, nil
}

func (s *FileApprovalStore) Put(ctx context.Context, serverName string, decision contract.ApprovalDecision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return fmt.Errorf("server name 不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.read()
	if err != nil {
		return err
	}
	items[serverName] = decision
	return s.write(items)
}

func (s *FileApprovalStore) read() (map[string]contract.ApprovalDecision, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]contract.ApprovalDecision{}, nil
		}
		return nil, fmt.Errorf("读取 mcp approvals 失败: %w", err)
	}
	if len(raw) == 0 {
		return map[string]contract.ApprovalDecision{}, nil
	}
	var items map[string]contract.ApprovalDecision
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("解析 mcp approvals 失败: %w", err)
	}
	if items == nil {
		items = map[string]contract.ApprovalDecision{}
	}
	return items, nil
}

func (s *FileApprovalStore) write(items map[string]contract.ApprovalDecision) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("创建 mcp approvals 目录失败: %w", err)
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("写入 mcp approvals 失败: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("提交 mcp approvals 失败: %w", err)
	}
	return nil
}

var _ contract.ApprovalStore = (*FileApprovalStore)(nil)
