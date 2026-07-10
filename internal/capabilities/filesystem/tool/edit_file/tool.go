// Package edit_file 实现 edit_file 工具。
package edit_file

import (
	"context"
	"fmt"
	"strings"

	"genesis-agent/internal/capabilities/filesystem/binarygate"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

// Tool 对文件做精确替换编辑。
type Tool struct {
	deps toolkit.Deps
}

type input struct {
	Path         string `json:"path"`
	OldString    string `json:"old_string"`
	NewString    string `json:"new_string"`
	ExpectedHash string `json:"expected_hash,omitempty"`
}

type output struct {
	Path         string `json:"path"`
	Replacements int    `json:"replacements"`
	Size         int64  `json:"size"`
	Hash         string `json:"hash"`
}

// New 创建 edit_file 工具。
func New(deps toolkit.Deps) (tool.Tool, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "edit_file",
		Description: "在当前 workspace 内或经审批的外部文件中执行精确字符串替换。old_string 必须且只能匹配一次。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"path":          {Type: "string", Description: "workspace 内或经审批的外部文件路径"},
				"old_string":    {Type: "string", Description: "要替换的原文，必须唯一匹配"},
				"new_string":    {Type: "string", Description: "替换后的文本"},
				"expected_hash": {Type: "string", Description: "可选，写前期望的当前文件 sha256"},
			},
			Required: []string{"path", "old_string", "new_string"},
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolkit.DecodeParams(params, &in); err != nil {
		return "", err
	}
	if in.OldString == "" {
		return "", fscontract.NewError(fscontract.ErrCodeInvalidInput, in.Path, fmt.Errorf("old_string不能为空"))
	}
	path, err := toolkit.ResolveRequire(ctx, t.deps, "edit_file", in.Path, permission.OperationEdit, fscontract.ResolveOptions{
		Operation: string(permission.OperationEdit),
		MustExist: true,
	})
	if err != nil {
		return "", err
	}
	release, err := toolkit.Acquire(ctx, t.deps.Locker, []scheduler.ResourceLock{
		{Scope: "workspace", Key: toolkit.WorkspaceLockKey(path), Mode: scheduler.LockWrite},
		{Scope: "file", Key: toolkit.FileLockKey(path), Mode: scheduler.LockWrite},
	})
	if err != nil {
		return "", err
	}
	defer release()

	stat, data, hash, err := t.current(ctx, path)
	if err != nil {
		return "", err
	}
	if _, err := t.deps.Freshness.CheckBeforeWrite(ctx, path, *stat, hash, in.ExpectedHash); err != nil {
		return "", err
	}
	content := string(data)
	count := strings.Count(content, in.OldString)
	if count != 1 {
		return "", fscontract.NewError(fscontract.ErrCodeInvalidInput, path.DisplayPath, fmt.Errorf("old_string匹配次数为%d，必须唯一匹配", count))
	}
	next := []byte(strings.Replace(content, in.OldString, in.NewString, 1))
	if err := binarygate.RejectFakeOfficeBinary(path.DisplayPath, next); err != nil {
		return "", err
	}
	if err := t.deps.Backend.Write(ctx, path, next, fscontract.WriteOptions{
		Overwrite:    true,
		Atomic:       true,
		ExpectedHash: in.ExpectedHash,
	}); err != nil {
		return "", err
	}
	nextStat, err := t.deps.Backend.Stat(ctx, path)
	if err != nil {
		return "", err
	}
	nextHash := toolkit.HashBytes(next)
	if err := t.deps.Freshness.RecordWrite(ctx, path, *nextStat, nextHash); err != nil {
		return "", err
	}
	return toolkit.ToJSON(output{Path: path.DisplayPath, Replacements: 1, Size: nextStat.Size, Hash: nextHash})
}

func (t *Tool) current(ctx context.Context, path model.ResolvedPath) (*model.FileStat, []byte, string, error) {
	stat, err := t.deps.Backend.Stat(ctx, path)
	if err != nil {
		return nil, nil, "", err
	}
	data, err := t.deps.Backend.Read(ctx, path, fscontract.ReadOptions{MaxBytes: stat.Size})
	if err != nil {
		return nil, nil, "", err
	}
	return stat, data, toolkit.HashBytes(data), nil
}
