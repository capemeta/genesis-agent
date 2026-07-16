// Package sandbox 提供基于 sandbox/workspace client 的 FileSystemBackend 适配。
package sandbox

import (
	"context"
	"fmt"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
)

// Backend 将 sandbox FileSystemClient 适配为 filesystem.FileSystemBackend。
type Backend struct {
	client    sandboxcontract.FileSystemClient
	workspace sandboxcontract.WorkspaceRef
}

// NewBackend 创建 sandbox 文件系统 backend。
func NewBackend(client sandboxcontract.FileSystemClient, workspace sandboxcontract.WorkspaceRef) (*Backend, error) {
	if client == nil {
		return nil, fmt.Errorf("sandbox FileSystemClient未配置")
	}
	if workspace.ID == "" {
		return nil, fmt.Errorf("sandbox workspace id不能为空")
	}
	return &Backend{client: client, workspace: workspace}, nil
}

func (b *Backend) Read(ctx context.Context, path fsmodel.ResolvedPath, opts fscontract.ReadOptions) ([]byte, error) {
	return b.client.ReadFile(ctx, sandboxcontract.FileRequest{Workspace: b.workspace, Path: path}, opts)
}

func (b *Backend) Write(ctx context.Context, path fsmodel.ResolvedPath, content []byte, opts fscontract.WriteOptions) error {
	return b.client.WriteFile(ctx, sandboxcontract.WriteFileRequest{Workspace: b.workspace, Path: path, Content: content, Options: opts})
}

func (b *Backend) ListDir(ctx context.Context, path fsmodel.ResolvedPath, opts fscontract.ListOptions) ([]fsmodel.DirEntry, error) {
	if !validListEntryType(opts.EntryType) {
		return nil, fscontract.NewError(fscontract.ErrCodeInvalidInput, path.DisplayPath, fmt.Errorf("不支持的entry_type: %s", opts.EntryType))
	}
	remoteOpts := opts
	remoteOpts.EntryType = ""
	if opts.EntryType != "" {
		// genesis-sandbox 线协议尚未声明类型筛选；请求其默认有界结果后在 adapter 内适配，避免泄漏产品内契约。
		remoteOpts.MaxEntries = 0
	}
	entries, err := b.client.ListDir(ctx, sandboxcontract.ListDirRequest{Workspace: b.workspace, Path: path, Options: remoteOpts})
	if err != nil || opts.EntryType == "" {
		return entries, err
	}
	filtered := make([]fsmodel.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Type == opts.EntryType {
			filtered = append(filtered, entry)
			if opts.MaxEntries > 0 && len(filtered) == opts.MaxEntries {
				break
			}
		}
	}
	return filtered, nil
}

func validListEntryType(entryType fsmodel.EntryType) bool {
	switch entryType {
	case "", fsmodel.EntryTypeDir, fsmodel.EntryTypeFile, fsmodel.EntryTypeSymlink, fsmodel.EntryTypeOther:
		return true
	default:
		return false
	}
}

func (b *Backend) Walk(ctx context.Context, path fsmodel.ResolvedPath, opts fscontract.WalkOptions) (*fsmodel.WalkOutcome, error) {
	return b.client.Walk(ctx, sandboxcontract.WalkRequest{Workspace: b.workspace, Path: path, Options: opts})
}

func (b *Backend) Stat(ctx context.Context, path fsmodel.ResolvedPath) (*fsmodel.FileStat, error) {
	return b.client.Stat(ctx, sandboxcontract.FileRequest{Workspace: b.workspace, Path: path})
}

func (b *Backend) MkdirAll(ctx context.Context, path fsmodel.ResolvedPath, opts fscontract.MkdirOptions) error {
	return b.client.MkdirAll(ctx, sandboxcontract.MkdirRequest{Workspace: b.workspace, Path: path, Options: opts})
}

func (b *Backend) Remove(ctx context.Context, path fsmodel.ResolvedPath, opts fscontract.RemoveOptions) error {
	return b.client.Remove(ctx, sandboxcontract.RemoveRequest{Workspace: b.workspace, Path: path, Options: opts})
}
