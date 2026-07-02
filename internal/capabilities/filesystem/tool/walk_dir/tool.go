// Package walk_dir 实现 walk_dir 工具。
package walk_dir

import (
	"context"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

// Tool 有边界地遍历目录。
type Tool struct {
	deps toolkit.Deps
}

type input struct {
	Path           string `json:"path"`
	MaxDepth       int    `json:"max_depth,omitempty"`
	MaxDirs        int    `json:"max_dirs,omitempty"`
	MaxEntries     int    `json:"max_entries,omitempty"`
	MaxBytes       int64  `json:"max_bytes,omitempty"`
	FollowSymlinks bool   `json:"follow_symlinks,omitempty"`
}

// New 创建 walk_dir 工具。
func New(deps toolkit.Deps) (tool.Tool, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "walk_dir",
		Description: "有边界地遍历当前 workspace 内或经审批的外部目录。默认限制深度、目录数、条目数和累计文件大小。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"path":            {Type: "string", Description: "workspace 内或经审批的外部目录路径"},
				"max_depth":       {Type: "integer", Description: "最大遍历深度"},
				"max_dirs":        {Type: "integer", Description: "最大目录数"},
				"max_entries":     {Type: "integer", Description: "最大条目数"},
				"max_bytes":       {Type: "integer", Description: "累计文件大小上限"},
				"follow_symlinks": {Type: "boolean", Description: "是否跟随符号链接目录，第一轮本地backend默认不跟随"},
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
	path, err := toolkit.ResolveRequire(ctx, t.deps, "walk_dir", in.Path, permission.OperationWalk, fscontract.ResolveOptions{
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
		Key:   toolkit.WorkspaceLockKey(path),
		Mode:  scheduler.LockRead,
	}})
	if err != nil {
		return "", err
	}
	defer release()

	out, err := t.deps.Backend.Walk(ctx, path, fscontract.WalkOptions{
		MaxDepth:       in.MaxDepth,
		MaxDirs:        in.MaxDirs,
		MaxEntries:     in.MaxEntries,
		MaxBytes:       in.MaxBytes,
		FollowSymlinks: in.FollowSymlinks,
	})
	if err != nil {
		return "", err
	}
	return toolkit.ToJSON(out)
}
