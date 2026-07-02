// Package read_file 实现 read_file 工具。
package read_file

import (
	"context"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

// Tool 读取文件。
type Tool struct {
	deps toolkit.Deps
}

type input struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

type output struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	Hash      string `json:"hash"`
	Truncated bool   `json:"truncated"`
}

// New 创建 read_file 工具。
func New(deps toolkit.Deps) (tool.Tool, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "read_file",
		Description: "读取当前 workspace 内或经审批的外部文本文件。必须提供 path，可用 max_bytes 限制最大读取字节数。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"path":      {Type: "string", Description: "workspace 内或经审批的外部文件路径"},
				"max_bytes": {Type: "integer", Description: "最大读取字节数，省略时使用默认限制"},
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
	path, err := toolkit.ResolveRequire(ctx, t.deps, "read_file", in.Path, permission.OperationRead, fscontract.ResolveOptions{
		Operation: string(permission.OperationRead),
		MustExist: true,
	})
	if err != nil {
		return "", err
	}
	release, err := toolkit.Acquire(ctx, t.deps.Locker, []scheduler.ResourceLock{{
		Scope: "file",
		Key:   toolkit.FileLockKey(path),
		Mode:  scheduler.LockRead,
	}})
	if err != nil {
		return "", err
	}
	defer release()

	data, readErr := t.deps.Backend.Read(ctx, path, fscontract.ReadOptions{MaxBytes: in.MaxBytes})
	truncated := fscontract.CodeOf(readErr) == fscontract.ErrCodeTooLarge
	if readErr != nil && !truncated {
		return "", readErr
	}
	stat, err := t.deps.Backend.Stat(ctx, path)
	if err != nil {
		return "", err
	}
	hash := toolkit.HashBytes(data)
	if !truncated {
		if err := t.deps.Freshness.RecordRead(ctx, path, *stat, hash); err != nil {
			return "", err
		}
	}
	return toolkit.ToJSON(output{
		Path:      path.DisplayPath,
		Content:   string(data),
		Size:      stat.Size,
		Hash:      hash,
		Truncated: truncated,
	})
}
