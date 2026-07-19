package permission

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"gopkg.in/yaml.v3"
)

const projectGrantsVersion = 1

// ProjectGrantStore 持久化项目级文件授权。
type ProjectGrantStore interface {
	Load(ctx context.Context) ([]RuntimeGrant, error)
	Save(ctx context.Context, grants []RuntimeGrant) error
}

type projectGrantsFile struct {
	Version int                 `yaml:"version"`
	Grants  []projectGrantEntry `yaml:"grants"`
}

type projectGrantEntry struct {
	Action string `yaml:"action"`
	Path   string `yaml:"path"`
	Scope  string `yaml:"scope"`
}

// FileProjectGrantStore 将 project grant 写入工作区 .genesis/grants.yaml。
type FileProjectGrantStore struct {
	path string
	mu   sync.Mutex
}

// NewFileProjectGrantStore 创建文件型项目授权存储。
func NewFileProjectGrantStore(path string) (*FileProjectGrantStore, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, fmt.Errorf("project grants 路径不能为空")
	}
	return &FileProjectGrantStore{path: path}, nil
}

// DefaultProjectGrantsPath 返回工作区默认 project grants 文件路径。
func DefaultProjectGrantsPath(workspaceRoot string) string {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		root = "."
	}
	return filepath.Join(filepath.Clean(root), ".genesis", "grants.yaml")
}

// NewRuntimeFilePermissionsWithProjectStore 创建已加载 .genesis/grants.yaml 的运行时授权集合。
func NewRuntimeFilePermissionsWithProjectStore(ctx context.Context, workspaceRoot string) (*RuntimeFilePermissions, error) {
	store, err := NewFileProjectGrantStore(DefaultProjectGrantsPath(workspaceRoot))
	if err != nil {
		return nil, err
	}
	perms := NewRuntimeFilePermissions()
	perms.SetProjectStore(store)
	if err := perms.LoadProject(ctx); err != nil {
		return nil, err
	}
	return perms, nil
}

// Path 返回存储文件路径。
func (s *FileProjectGrantStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load 读取已持久化的 project grant；文件不存在时返回空列表。
func (s *FileProjectGrantStore) Load(ctx context.Context) ([]RuntimeGrant, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 project grants 失败: %w", err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var file projectGrantsFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("解析 project grants 失败: %w", err)
	}
	out := make([]RuntimeGrant, 0, len(file.Grants))
	for _, entry := range file.Grants {
		action := approvalmodel.Action(strings.TrimSpace(entry.Action))
		path := normalizeGrantPath(entry.Path)
		scope := approvalmodel.GrantScope(strings.ToLower(strings.TrimSpace(entry.Scope)))
		if action == "" || path == "" {
			continue
		}
		if scope == "" {
			scope = approvalmodel.GrantScopeProject
		}
		if scope != approvalmodel.GrantScopeProject {
			continue
		}
		out = append(out, RuntimeGrant{Action: action, Scope: scope, Path: path})
	}
	return out, nil
}

// Save 原子写入全部 project grant。
func (s *FileProjectGrantStore) Save(ctx context.Context, grants []RuntimeGrant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := make([]projectGrantEntry, 0, len(grants))
	for _, grant := range grants {
		if grant.Scope != approvalmodel.GrantScopeProject {
			continue
		}
		path := normalizeGrantPath(grant.Path)
		if grant.Action == "" || path == "" {
			continue
		}
		entries = append(entries, projectGrantEntry{
			Action: string(grant.Action),
			Path:   path,
			Scope:  string(approvalmodel.GrantScopeProject),
		})
	}
	file := projectGrantsFile{Version: projectGrantsVersion, Grants: entries}
	raw, err := yaml.Marshal(&file)
	if err != nil {
		return fmt.Errorf("序列化 project grants 失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("创建 project grants 目录失败: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("写入 project grants 失败: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("提交 project grants 失败: %w", err)
	}
	return nil
}
