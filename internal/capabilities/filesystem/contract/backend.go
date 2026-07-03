package contract

import (
	"context"

	"genesis-agent/internal/capabilities/filesystem/model"
)

// PathResolver 将用户输入路径解析为 backend 可访问的安全路径。
type PathResolver interface {
	Resolve(ctx context.Context, ref model.PathRef, opts ResolveOptions) (model.ResolvedPath, error)
}

// FileSystemBackend 是文件系统工具唯一依赖的真实访问端口。
type FileSystemBackend interface {
	Read(ctx context.Context, path model.ResolvedPath, opts ReadOptions) ([]byte, error)
	Write(ctx context.Context, path model.ResolvedPath, content []byte, opts WriteOptions) error
	ListDir(ctx context.Context, path model.ResolvedPath, opts ListOptions) ([]model.DirEntry, error)
	Walk(ctx context.Context, path model.ResolvedPath, opts WalkOptions) (*model.WalkOutcome, error)
	Stat(ctx context.Context, path model.ResolvedPath) (*model.FileStat, error)
	MkdirAll(ctx context.Context, path model.ResolvedPath, opts MkdirOptions) error
	Remove(ctx context.Context, path model.ResolvedPath, opts RemoveOptions) error
}
