// Package list_dir 实现 list_dir 工具。
package list_dir

import (
	"context"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

// Tool 枚举目录。
type Tool struct {
	deps toolkit.Deps
}

type input struct {
	Path       string `json:"path"`
	MaxEntries int    `json:"max_entries,omitempty"`
}

type output struct {
	Path    string           `json:"path"`
	Entries []model.DirEntry `json:"entries"`
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
		Description: "列出当前 workspace 内或经审批的外部目录的直接子项。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"path":        {Type: "string", Description: "workspace 内或经审批的外部目录路径"},
				"max_entries": {Type: "integer", Description: "最多返回的目录项数量"},
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

	entries, err := t.deps.Backend.ListDir(ctx, path, fscontract.ListOptions{MaxEntries: in.MaxEntries})
	if err != nil {
		return "", err
	}
	return toolkit.ToJSON(output{Path: path.DisplayPath, Entries: entries})
}
