// Package glob 实现 glob 文件匹配工具。
package glob

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

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
	OK         bool     `json:"ok"`
	Root       string   `json:"root"`
	Pattern    string   `json:"pattern"`
	Matches    []string `json:"matches"`
	MatchCount int      `json:"match_count"`
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
		Name: "glob",
		Description: "仅在文件位置未知或需要通配符匹配时，按 glob pattern 查找路径。" +
			"用户给出的裸文件名（如 report.md）也是 workspace 根下的精确相对路径，应直接调用 read_file，禁止改写成 **/report.md。" +
			"支持 *, ?, **；返回 matches 路径数组与 match_count。matches=[] 表示无匹配，仍 ok=true，不是失败。" +
			"禁止用 run_command 执行 ls/dir 通配来枚举文件。无通配符时走精确路径快查。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"pattern":      {Type: "string", Description: "位置未知时使用的 glob pattern（如 *.go 或 **/*.pptx）；避免一次性发起的多次重叠通配符查询。已知精确路径时直接使用 read_file"},
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

	if isExactGlobPattern(in.Pattern) {
		return t.executeExact(ctx, in, rootRaw)
	}
	matcher, err := search.NewGlobMatcher(in.Pattern)
	if err != nil {
		return "", fscontract.NewError(fscontract.ErrCodeInvalidInput, in.Pattern, err)
	}
	maxResults := in.MaxResults
	if maxResults <= 0 {
		maxResults = defaultGlobMaxResults
	}
	walk, err := t.deps.Backend.Walk(ctx, root, fscontract.WalkOptions{MaxDepth: in.MaxDepth, MaxEntries: maxResults * 20, ExcludeDirs: toolkit.NoiseDirsExceptExplicitPattern(in.Pattern)})
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
				return toolkit.ToJSON(globResult(root.DisplayPath, in.Pattern, matches, true, "max_results"))
			}
		}
	}
	return toolkit.ToJSON(globResult(root.DisplayPath, in.Pattern, matches, walk.Truncated, walk.LimitCause))
}

func (t *Tool) executeExact(ctx context.Context, in input, rootRaw string) (string, error) {
	raw := strings.TrimSpace(in.Pattern)
	if raw == "" {
		return toolkit.ToJSON(globResult("", in.Pattern, nil, false, ""))
	}
	if !filepath.IsAbs(raw) && strings.TrimSpace(rootRaw) != "" && strings.TrimSpace(rootRaw) != "." {
		raw = filepath.Join(rootRaw, raw)
	}
	resolved, err := toolkit.ResolveRequire(ctx, t.deps, "glob", raw, permission.OperationWalk, fscontract.ResolveOptions{
		Operation:      string(permission.OperationWalk),
		MustExist:      true,
		AllowDirectory: true,
	})
	if err != nil {
		if fscontract.CodeOf(err) == fscontract.ErrCodeNotFound {
			return toolkit.ToJSON(globResult("", in.Pattern, nil, false, ""))
		}
		return "", err
	}
	stat, err := t.deps.Backend.Stat(ctx, resolved)
	if err != nil {
		if fscontract.CodeOf(err) == fscontract.ErrCodeNotFound {
			return toolkit.ToJSON(globResult("", in.Pattern, nil, false, ""))
		}
		return "", err
	}
	if !in.IncludeDirs && stat.Type == model.EntryTypeDir {
		return toolkit.ToJSON(globResult("", in.Pattern, nil, false, ""))
	}
	return toolkit.ToJSON(globResult("", in.Pattern, []string{resolved.DisplayPath}, false, ""))
}

func globResult(root, pattern string, matches []string, truncated bool, limitCause string) output {
	if matches == nil {
		matches = []string{}
	}
	return output{
		OK:         true,
		Root:       root,
		Pattern:    pattern,
		Matches:    matches,
		MatchCount: len(matches),
		Truncated:  truncated,
		LimitCause: limitCause,
	}
}

func isExactGlobPattern(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	return pattern != "" && !strings.ContainsAny(pattern, "*?[")
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
