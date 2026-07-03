// Package glob 实现 glob 文件匹配工具。
package glob

import (
	"context"
	"fmt"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/search"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

const defaultGlobMaxResults = 200

// Tool 按 glob pattern 匹配文件路径。
type Tool struct {
	deps toolkit.Deps
}

type input struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path,omitempty"`
	MaxDepth    int    `json:"max_depth,omitempty"`
	MaxResults  int    `json:"max_results,omitempty"`
	IncludeDirs bool   `json:"include_dirs,omitempty"`
}

type output struct {
	Root       string   `json:"root"`
	Pattern    string   `json:"pattern"`
	Matches    []string `json:"matches"`
	Truncated  bool     `json:"truncated"`
	LimitCause string   `json:"limit_cause,omitempty"`
}

// New 创建 glob 工具。
func New(deps toolkit.Deps) (tool.Tool, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "glob",
		Description: "在当前 workspace 内或经审批的目录中按 glob pattern 查找路径。支持 *, ?, **，返回结构化路径列表。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"pattern":      {Type: "string", Description: "glob pattern，例如 *.go 或 **/*.go"},
				"path":         {Type: "string", Description: "搜索根目录，默认当前 workspace"},
				"max_depth":    {Type: "integer", Description: "最大搜索深度"},
				"max_results":  {Type: "integer", Description: "最大返回结果数，默认200"},
				"include_dirs": {Type: "boolean", Description: "是否包含目录结果，默认只返回文件和符号链接"},
			},
			Required: []string{"pattern"},
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolkit.DecodeParams(params, &in); err != nil {
		return "", err
	}
	if in.Pattern == "" {
		return "", fscontract.NewError(fscontract.ErrCodeInvalidInput, "", fmt.Errorf("pattern不能为空"))
	}
	rootRaw := in.Path
	if rootRaw == "" {
		rootRaw = "."
	}
	root, err := toolkit.ResolveRequire(ctx, t.deps, "glob", rootRaw, permission.OperationWalk, fscontract.ResolveOptions{
		Operation:        string(permission.OperationWalk),
		MustExist:        true,
		AllowDirectory:   true,
		RequireDirectory: true,
	})
	if err != nil {
		return "", err
	}
	release, err := toolkit.Acquire(ctx, t.deps.Locker, []scheduler.ResourceLock{{
		Scope: "workspace",
		Key:   toolkit.WorkspaceLockKey(root),
		Mode:  scheduler.LockRead,
	}})
	if err != nil {
		return "", err
	}
	defer release()

	matcher, err := search.NewGlobMatcher(in.Pattern)
	if err != nil {
		return "", fscontract.NewError(fscontract.ErrCodeInvalidInput, in.Pattern, err)
	}
	maxResults := in.MaxResults
	if maxResults <= 0 {
		maxResults = defaultGlobMaxResults
	}
	walk, err := t.deps.Backend.Walk(ctx, root, fscontract.WalkOptions{MaxDepth: in.MaxDepth, MaxEntries: maxResults * 20})
	if err != nil {
		return "", err
	}
	matches := make([]string, 0)
	for _, entry := range walk.Entries {
		if !in.IncludeDirs && entry.Type == model.EntryTypeDir {
			continue
		}
		candidate := entry.Path
		if root.DisplayPath != "" && root.DisplayPath != "." {
			candidate = trimRoot(candidate, root.DisplayPath)
		}
		if matcher.Match(candidate) || matcher.Match(entry.Path) {
			matches = append(matches, entry.Path)
			if len(matches) >= maxResults {
				return toolkit.ToJSON(output{Root: root.DisplayPath, Pattern: in.Pattern, Matches: matches, Truncated: true, LimitCause: "max_results"})
			}
		}
	}
	return toolkit.ToJSON(output{Root: root.DisplayPath, Pattern: in.Pattern, Matches: matches, Truncated: walk.Truncated, LimitCause: walk.LimitCause})
}

func trimRoot(path string, root string) string {
	if path == root {
		return "."
	}
	prefix := root + "/"
	if len(path) > len(prefix) && path[:len(prefix)] == prefix {
		return path[len(prefix):]
	}
	return path
}
