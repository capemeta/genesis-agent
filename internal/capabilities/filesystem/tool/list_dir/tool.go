// Package list_dir 实现 list_dir 工具。
package list_dir

import (
	"context"
	"fmt"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

const (
	defaultMaxEntries = 1000
	maxEntriesLimit   = 9999 // 预留一个后端探测位，用于准确判断truncated。
)

// Tool 枚举目录。
type Tool struct {
	deps toolkit.Deps
}

type input struct {
	Path       string `json:"path"`
	MaxEntries int    `json:"max_entries,omitempty"`
	EntryType  string `json:"entry_type,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type output struct {
	Path          string    `json:"path"`
	EntryType     string    `json:"entry_type"`
	Detail        string    `json:"detail"`
	ReturnedCount int       `json:"returned_count"`
	Truncated     bool      `json:"truncated"`
	Names         *[]string `json:"names,omitempty"`
	Entries       any       `json:"entries,omitempty"`
}

type compactEntry struct {
	Name string          `json:"name"`
	Path string          `json:"path"`
	Type model.EntryType `json:"type"`
}

// New 创建 list_dir 工具。
func New(deps toolkit.Deps) (tool.Tool, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "list_dir",
		Description: "列出当前 workspace 内或经审批的外部目录的直接子项，可按类型筛选。returned_count是本次准确返回数量，禁止自行计数；truncated=true表示结果不完整。只需要名称时使用detail=names。列目录时应优先使用本工具，不要用run_command执行ls、dir或Get-ChildItem。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"path":        {Type: "string", Description: "workspace 内或经审批的外部目录路径"},
				"max_entries": {Type: "integer", Description: "最多返回的目录项数量，默认1000，最大9999"},
				"entry_type":  {Type: "string", Description: "可选的目录项类型筛选", Enum: []string{"all", "dir", "file", "symlink", "other"}},
				"detail":      {Type: "string", Description: "返回详情：names只返回名称；compact返回名称、路径和类型；full返回完整元数据。默认compact", Enum: []string{"names", "compact", "full"}},
			},
			Required: []string{"path"},
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolkit.DecodeParams(params, &in); err != nil {
		return "", err
	}
	if _, err := filterEntries(nil, in.EntryType); err != nil {
		return "", err
	}
	detail, err := normalizeDetail(in.Detail)
	if err != nil {
		return "", err
	}
	maxEntries, err := normalizeMaxEntries(in.MaxEntries)
	if err != nil {
		return "", err
	}
	path, err := toolkit.ResolveRequire(ctx, t.deps, "list_dir", in.Path, permission.OperationList, fscontract.ResolveOptions{
		Operation:        string(permission.OperationList),
		MustExist:        true,
		AllowDirectory:   true,
		RequireDirectory: true,
	})
	if err != nil {
		return "", err
	}
	release, err := toolkit.Acquire(ctx, t.deps.Locker, []scheduler.ResourceLock{{
		Scope: "workspace",
		Key:   toolkit.WorkspaceLockKey(path),
		Mode:  scheduler.LockRead,
	}})
	if err != nil {
		return "", err
	}
	defer release()

	entryType := model.EntryType(in.EntryType)
	if in.EntryType == "all" {
		entryType = ""
	}
	entries, err := t.deps.Backend.ListDir(ctx, path, fscontract.ListOptions{MaxEntries: maxEntries + 1, EntryType: entryType})
	if err != nil {
		return "", err
	}
	entries, err = filterEntries(entries, in.EntryType)
	if err != nil {
		return "", err
	}
	entries, truncated := truncateEntries(entries, maxEntries)
	return toolkit.ToJSON(buildOutput(path.DisplayPath, normalizedEntryType(in.EntryType), detail, entries, truncated))
}

func normalizeMaxEntries(value int) (int, error) {
	if value < 0 || value > maxEntriesLimit {
		return 0, fscontract.NewError(fscontract.ErrCodeInvalidInput, "", fmt.Errorf("max_entries必须在1到%d之间，或省略使用默认值", maxEntriesLimit))
	}
	if value == 0 {
		return defaultMaxEntries, nil
	}
	return value, nil
}

func normalizeDetail(value string) (string, error) {
	if value == "" {
		return "compact", nil
	}
	switch value {
	case "names", "compact", "full":
		return value, nil
	default:
		return "", fscontract.NewError(fscontract.ErrCodeInvalidInput, "", fmt.Errorf("detail必须是names、compact或full"))
	}
}

func normalizedEntryType(value string) string {
	if value == "" {
		return "all"
	}
	return value
}

func truncateEntries(entries []model.DirEntry, maxEntries int) ([]model.DirEntry, bool) {
	if len(entries) <= maxEntries {
		return entries, false
	}
	return entries[:maxEntries], true
}

func buildOutput(path, entryType, detail string, entries []model.DirEntry, truncated bool) output {
	out := output{
		Path:          path,
		EntryType:     entryType,
		Detail:        detail,
		ReturnedCount: len(entries),
		Truncated:     truncated,
	}
	switch detail {
	case "names":
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name)
		}
		out.Names = &names
	case "full":
		out.Entries = entries
	default:
		compact := make([]compactEntry, 0, len(entries))
		for _, entry := range entries {
			compact = append(compact, compactEntry{Name: entry.Name, Path: entry.Path, Type: entry.Type})
		}
		out.Entries = compact
	}
	return out
}

func filterEntries(entries []model.DirEntry, rawType string) ([]model.DirEntry, error) {
	want := model.EntryType(rawType)
	if rawType == "" || rawType == "all" {
		return entries, nil
	}
	switch want {
	case model.EntryTypeDir, model.EntryTypeFile, model.EntryTypeSymlink, model.EntryTypeOther:
	default:
		return nil, fscontract.NewError(fscontract.ErrCodeInvalidInput, "", fmt.Errorf("entry_type必须是all、dir、file、symlink或other"))
	}
	filtered := make([]model.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Type == want {
			filtered = append(filtered, entry)
		}
	}
	return filtered, nil
}
