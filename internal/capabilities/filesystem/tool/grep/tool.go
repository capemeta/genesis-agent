// Package grep 实现 grep 内容搜索工具。
package grep

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/search"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

const (
	defaultMaxMatches = 100
	defaultMaxFiles   = 200
	defaultMaxBytes   = int64(1024 * 1024)
)

// Tool 按正则搜索文件内容。
type Tool struct {
	deps toolkit.Deps
}

type input struct {
	Pattern       string `json:"pattern"`
	Path          string `json:"path,omitempty"`
	Include       string `json:"include,omitempty"`
	CaseSensitive *bool  `json:"case_sensitive,omitempty"`
	MaxMatches    int    `json:"max_matches,omitempty"`
	MaxFiles      int    `json:"max_files,omitempty"`
	MaxBytes      int64  `json:"max_bytes,omitempty"`
}

type match struct {
	Path       string `json:"path"`
	LineNumber int    `json:"line_number"`
	Line       string `json:"line"`
}

type output struct {
	Root       string  `json:"root"`
	Pattern    string  `json:"pattern"`
	Matches    []match `json:"matches"`
	FilesSeen  int     `json:"files_seen"`
	Truncated  bool    `json:"truncated"`
	LimitCause string  `json:"limit_cause,omitempty"`
}

// New 创建 grep 工具。
func New(deps toolkit.Deps) (tool.Tool, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "grep",
		Description: "在当前 workspace 内或经审批的目录中按正则搜索文本内容。默认跳过二进制文件并限制结果数量。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"pattern":        {Type: "string", Description: "正则表达式 pattern"},
				"path":           {Type: "string", Description: "搜索根目录，默认当前 workspace"},
				"include":        {Type: "string", Description: "可选 glob 过滤，例如 **/*.go"},
				"case_sensitive": {Type: "boolean", Description: "是否大小写敏感，默认 true"},
				"max_matches":    {Type: "integer", Description: "最大匹配行数，默认100"},
				"max_files":      {Type: "integer", Description: "最大扫描文件数，默认200"},
				"max_bytes":      {Type: "integer", Description: "单文件最大读取字节数，默认1MiB"},
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
	pattern := in.Pattern
	if in.CaseSensitive != nil && !*in.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fscontract.NewError(fscontract.ErrCodeInvalidInput, in.Pattern, err)
	}
	var include *search.GlobMatcher
	if in.Include != "" {
		include, err = search.NewGlobMatcher(in.Include)
		if err != nil {
			return "", fscontract.NewError(fscontract.ErrCodeInvalidInput, in.Include, err)
		}
	}
	rootRaw := in.Path
	if rootRaw == "" {
		rootRaw = "."
	}
	root, err := toolkit.ResolveRequire(ctx, t.deps, "grep", rootRaw, permission.OperationWalk, fscontract.ResolveOptions{
		Operation:        string(permission.OperationWalk),
		MustExist:        true,
		AllowDirectory:   true,
		RequireDirectory: true,
	})
	if err != nil {
		return "", err
	}
	release, err := toolkit.Acquire(ctx, t.deps.Locker, []scheduler.ResourceLock{{Scope: "workspace", Key: toolkit.WorkspaceLockKey(root), Mode: scheduler.LockRead}})
	if err != nil {
		return "", err
	}
	defer release()

	maxMatches := defaulted(in.MaxMatches, defaultMaxMatches)
	maxFiles := defaulted(in.MaxFiles, defaultMaxFiles)
	maxBytes := in.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	walk, err := t.deps.Backend.Walk(ctx, root, fscontract.WalkOptions{MaxEntries: maxFiles * 20, MaxBytes: maxBytes * int64(maxFiles)})
	if err != nil {
		return "", err
	}
	out := output{Root: root.DisplayPath, Pattern: in.Pattern, Matches: make([]match, 0), Truncated: walk.Truncated, LimitCause: walk.LimitCause}
	for _, entry := range walk.Entries {
		if entry.Type != model.EntryTypeFile {
			continue
		}
		if include != nil && !include.Match(entry.Path) {
			continue
		}
		out.FilesSeen++
		if out.FilesSeen > maxFiles {
			out.Truncated = true
			out.LimitCause = "max_files"
			break
		}
		path, err := t.deps.Resolver.Resolve(ctx, model.PathRef{Raw: entry.Path}, fscontract.ResolveOptions{Operation: string(permission.OperationRead), MustExist: true})
		if err != nil {
			continue
		}
		data, err := t.deps.Backend.Read(ctx, path, fscontract.ReadOptions{MaxBytes: maxBytes})
		if err != nil && fscontract.CodeOf(err) != fscontract.ErrCodeTooLarge {
			continue
		}
		if search.IsProbablyBinary(data) {
			continue
		}
		appendMatches(re, entry.Path, string(data), &out, maxMatches)
		if len(out.Matches) >= maxMatches {
			out.Truncated = true
			out.LimitCause = "max_matches"
			break
		}
	}
	return toolkit.ToJSON(out)
}

func appendMatches(re *regexp.Regexp, p string, content string, out *output, maxMatches int) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if re.MatchString(line) {
			out.Matches = append(out.Matches, match{Path: p, LineNumber: i + 1, Line: line})
			if len(out.Matches) >= maxMatches {
				return
			}
		}
	}
}

func defaulted(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
