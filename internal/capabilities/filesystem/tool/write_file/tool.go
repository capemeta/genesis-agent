// Package write_file 实现 write_file 工具。
package write_file

import (
	"context"
	"fmt"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/binarygate"
	"genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

// Tool 写入文件。
type Tool struct {
	deps toolkit.Deps
}

type input struct {
	Path          string `json:"path"`
	Content       string `json:"content"`
	CreateParents bool   `json:"create_parents,omitempty"`
	Overwrite     *bool  `json:"overwrite,omitempty"`
	ExpectedHash  string `json:"expected_hash,omitempty"`
}

type output struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Hash string `json:"hash"`
}

// New 创建 write_file 工具。
func New(deps toolkit.Deps) (tool.Tool, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "write_file",
		Description: "写入当前 workspace 内或经审批的外部文件。默认允许覆盖并使用原子写；可传 create_parents、overwrite=false、expected_hash。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"path":           {Type: "string", Description: "workspace 内或经审批的外部文件路径"},
				"content":        {Type: "string", Description: "要写入的完整文件内容"},
				"create_parents": {Type: "boolean", Description: "是否创建父目录"},
				"overwrite":      {Type: "boolean", Description: "是否允许覆盖已有文件，默认true"},
				"expected_hash":  {Type: "string", Description: "可选，写前期望的当前文件 sha256"},
			},
			Required: []string{"path", "content"},
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolkit.DecodeParams(params, &in); err != nil {
		return "", err
	}
	path, err := toolkit.ResolveRequire(ctx, t.deps, "write_file", in.Path, permission.OperationWrite, fscontract.ResolveOptions{
		Operation: string(permission.OperationWrite),
		MustExist: false,
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

	overwrite := true
	if in.Overwrite != nil {
		overwrite = *in.Overwrite
	}
	if stat, currentHash, ok, err := t.current(ctx, path); err != nil {
		return "", err
	} else if ok {
		if !overwrite {
			return "", fscontract.NewError(fscontract.ErrCodeAlreadyExists, path.DisplayPath, fmt.Errorf("文件已存在且overwrite=false"))
		}
		if _, err := t.deps.Freshness.CheckBeforeWrite(ctx, path, *stat, currentHash, in.ExpectedHash); err != nil {
			return "", err
		}
	} else if in.ExpectedHash != "" {
		return "", fscontract.NewError(fscontract.ErrCodeNotFound, path.DisplayPath, fmt.Errorf("expected_hash要求目标文件已存在"))
	}
	content := []byte(in.Content)
	if err := binarygate.RejectFakeOfficeBinary(path.DisplayPath, content); err != nil {
		return "", err
	}
	if err := t.deps.Backend.Write(ctx, path, content, fscontract.WriteOptions{
		CreateParents: in.CreateParents,
		Overwrite:     overwrite,
		Atomic:        true,
		ExpectedHash:  in.ExpectedHash,
	}); err != nil {
		return "", err
	}
	stat, err := t.deps.Backend.Stat(ctx, path)
	if err != nil {
		return "", err
	}
	hash := toolkit.HashBytes(content)
	if err := t.deps.Freshness.RecordWrite(ctx, path, *stat, hash); err != nil {
		return "", err
	}
	return toolkit.ToJSON(output{Path: path.DisplayPath, Size: stat.Size, Hash: hash})
}

func (t *Tool) current(ctx context.Context, path model.ResolvedPath) (*model.FileStat, string, bool, error) {
	stat, err := t.deps.Backend.Stat(ctx, path)
	if err != nil {
		if fscontract.CodeOf(err) == fscontract.ErrCodeNotFound {
			return nil, "", false, nil
		}
		return nil, "", false, err
	}
	data, err := t.deps.Backend.Read(ctx, path, fscontract.ReadOptions{MaxBytes: stat.Size})
	if err != nil {
		return nil, "", false, err
	}
	return stat, toolkit.HashBytes(data), true, nil
}
