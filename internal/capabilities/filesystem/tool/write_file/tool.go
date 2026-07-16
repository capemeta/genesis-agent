// Package write_file 实现 write_file 工具。
package write_file

import (
	"context"
	"fmt"

	"genesis-agent/internal/capabilities/filesystem/binarygate"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
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
	Append        bool   `json:"append,omitempty"`
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
		Name: "write_file",
		Description: "原子写入文本文件。" +
			"⚠️ 重要：content 参数作为 JSON 字符串传输，超长内容会被 LLM 在输出中途截断导致 unexpected EOF；" +
			"当 content 预估超过 2000 词（约 8000 字符）时，必须拆分：优先使用 apply_patch 的 Add File 操作创建大文件，" +
			"或首次写入骨架后多次 append=true 追加各段（append 时须提供上次返回的 hash 作 expected_hash）。" +
			"不要原样重试被截断的调用，改变策略拆分后再试。" +
			"Run 中间脚本请写 $WORK_DIR/...，最终交付写 $OUTPUT_DIR/...，禁止写仓库根。" +
			"修改已有文件优先用 apply_patch。默认允许覆盖；可传 create_parents、overwrite=false、expected_hash。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"path":           {Type: "string", Description: "workspace 内或经审批的外部文件路径；Run 中间脚本/跨步骤状态用 $WORK_DIR/...，用户要求生成的最终文本交付物用 $OUTPUT_DIR/...；用户明确指定目标路径时按指定路径写入（勿写仓库根）"},
				"content":        {Type: "string", Description: "要写入的文件内容；超过约 8000 字符时请改用 apply_patch Add File 或多次 append，避免 JSON 传输截断"},
				"create_parents": {Type: "boolean", Description: "是否创建父目录"},
				"overwrite":      {Type: "boolean", Description: "是否允许覆盖已有文件，默认true"},
				"append":         {Type: "boolean", Description: "追加到已有文件；必须同时提供该文件当前的 expected_hash，整个结果仍原子写入"},
				"expected_hash":  {Type: "string", Description: "可选，写前期望的当前文件 sha256；append 模式必填"},
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
	if in.Append && in.ExpectedHash == "" {
		return "", fmt.Errorf("append=true时expected_hash不能为空")
	}
	var existing []byte
	if stat, currentHash, ok, err := t.current(ctx, path); err != nil {
		return "", err
	} else if ok {
		if !overwrite {
			return "", fscontract.NewError(fscontract.ErrCodeAlreadyExists, path.DisplayPath, fmt.Errorf("文件已存在且overwrite=false"))
		}
		if _, err := t.deps.Freshness.CheckBeforeWrite(ctx, path, *stat, currentHash, in.ExpectedHash); err != nil {
			return "", err
		}
		if in.Append {
			existing, err = t.deps.Backend.Read(ctx, path, fscontract.ReadOptions{MaxBytes: stat.Size})
			if err != nil {
				return "", err
			}
		}
	} else if in.ExpectedHash != "" {
		return "", fscontract.NewError(fscontract.ErrCodeNotFound, path.DisplayPath, fmt.Errorf("expected_hash要求目标文件已存在"))
	} else if in.Append {
		return "", fscontract.NewError(fscontract.ErrCodeNotFound, path.DisplayPath, fmt.Errorf("append要求目标文件已存在"))
	}
	content := composeWriteContent(existing, []byte(in.Content), in.Append)
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

func composeWriteContent(existing, incoming []byte, appendMode bool) []byte {
	if !appendMode {
		return incoming
	}
	combined := make([]byte, 0, len(existing)+len(incoming))
	combined = append(combined, existing...)
	combined = append(combined, incoming...)
	return combined
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
