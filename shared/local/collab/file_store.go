// Package collab 提供本地文件协作模式 ModeStore。
package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	runtimecollab "genesis-agent/internal/runtime/collab"
)

var safeSession = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// FileStore 将会话协作状态持久化到 JSON 文件。
type FileStore struct {
	mu      sync.Mutex
	baseDir string
}

// NewFileStore 创建文件 ModeStore；目录不存在则创建。
func NewFileStore(baseDir string) (*FileStore, error) {
	baseDir = filepath.Clean(baseDir)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create collab store dir: %w", err)
	}
	return &FileStore{baseDir: baseDir}, nil
}

func (s *FileStore) path(sessionID string) string {
	id := safeSession.ReplaceAllString(sessionID, "_")
	if id == "" {
		id = "session"
	}
	return filepath.Join(s.baseDir, id+".json")
}

// Get 读取会话协作状态。
func (s *FileStore) Get(_ context.Context, sessionID string) (runtimecollab.SessionState, error) {
	if s == nil || sessionID == "" {
		return runtimecollab.SessionState{Mode: runtimecollab.ModeDefault}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path(sessionID))
	if os.IsNotExist(err) {
		return runtimecollab.SessionState{Mode: runtimecollab.ModeDefault}, nil
	}
	if err != nil {
		return runtimecollab.SessionState{}, fmt.Errorf("read collab state: %w", err)
	}
	var st runtimecollab.SessionState
	if err := json.Unmarshal(data, &st); err != nil {
		return runtimecollab.SessionState{}, fmt.Errorf("decode collab state: %w", err)
	}
	st.Mode = runtimecollab.Normalize(st.Mode)
	return st, nil
}

// Set 写入会话协作状态。
func (s *FileStore) Set(_ context.Context, sessionID string, state runtimecollab.SessionState) error {
	if s == nil || sessionID == "" {
		return fmt.Errorf("collab store: empty session id")
	}
	state.Mode = runtimecollab.Normalize(state.Mode)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode collab state: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path(sessionID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write collab state tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename collab state: %w", err)
	}
	return nil
}

// WritePlanDocument 将实施方案写入工作区相对路径 `.genesis/plans/<session>.md`。
func WritePlanDocument(workspaceRoot, sessionID, content string) (relPath string, err error) {
	relPath = runtimecollab.PlanDocumentRelPath(sessionID)
	root := filepath.Clean(workspaceRoot)
	if root == "" {
		root = "."
	}
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("create plans dir: %w", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write plan document: %w", err)
	}
	return relPath, nil
}

// ReadPlanDocument 读取实施方案；不存在返回空内容。
func ReadPlanDocument(workspaceRoot, sessionID string) (relPath, content string, err error) {
	relPath = runtimecollab.PlanDocumentRelPath(sessionID)
	root := filepath.Clean(workspaceRoot)
	if root == "" {
		root = "."
	}
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	data, err := os.ReadFile(abs)
	if os.IsNotExist(err) {
		return relPath, "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read plan document: %w", err)
	}
	return relPath, string(data), nil
}
