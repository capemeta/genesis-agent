// Package contract 定义产品无关 sandbox/workspace 端口。
package contract

import (
	"context"
	"io"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
)

// WorkspaceRef 标识 sandbox/workspace 服务中的工作区。
type WorkspaceRef struct {
	ID       string            `json:"id"`
	Provider string            `json:"provider,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// FileRequest 是 sandbox 文件读取/状态请求。
type FileRequest struct {
	Workspace WorkspaceRef         `json:"workspace"`
	Path      fsmodel.ResolvedPath `json:"path"`
}

// WriteFileRequest 是 sandbox 文件写入请求。
type WriteFileRequest struct {
	Workspace WorkspaceRef            `json:"workspace"`
	Path      fsmodel.ResolvedPath    `json:"path"`
	Content   []byte                  `json:"-"`
	Options   fscontract.WriteOptions `json:"options"`
}

// ListDirRequest 是 sandbox 目录枚举请求。
type ListDirRequest struct {
	Workspace WorkspaceRef           `json:"workspace"`
	Path      fsmodel.ResolvedPath   `json:"path"`
	Options   fscontract.ListOptions `json:"options"`
}

// WalkRequest 是 sandbox bounded walk 请求。
type WalkRequest struct {
	Workspace WorkspaceRef           `json:"workspace"`
	Path      fsmodel.ResolvedPath   `json:"path"`
	Options   fscontract.WalkOptions `json:"options"`
}

// MkdirRequest 是 sandbox 创建目录请求。
type MkdirRequest struct {
	Workspace WorkspaceRef            `json:"workspace"`
	Path      fsmodel.ResolvedPath    `json:"path"`
	Options   fscontract.MkdirOptions `json:"options"`
}

// RemoveRequest 是 sandbox 文件删除请求。
type RemoveRequest struct {
	Workspace WorkspaceRef             `json:"workspace"`
	Path      fsmodel.ResolvedPath     `json:"path"`
	Options   fscontract.RemoveOptions `json:"options"`
}

// CommandRequest 是 sandbox 命令执行请求。
type CommandRequest struct {
	Workspace WorkspaceRef             `json:"workspace"`
	Command   execmodel.Command        `json:"command"`
	Sandbox   execmodel.SandboxProfile `json:"sandbox"`
	Options   execcontract.RunOptions  `json:"options"`
}

// FileSystemClient 是 sandbox/workspace 文件访问端口。
type FileSystemClient interface {
	ReadFile(ctx context.Context, req FileRequest, opts fscontract.ReadOptions) ([]byte, error)
	WriteFile(ctx context.Context, req WriteFileRequest) error
	ListDir(ctx context.Context, req ListDirRequest) ([]fsmodel.DirEntry, error)
	Walk(ctx context.Context, req WalkRequest) (*fsmodel.WalkOutcome, error)
	Stat(ctx context.Context, req FileRequest) (*fsmodel.FileStat, error)
	MkdirAll(ctx context.Context, req MkdirRequest) error
	Remove(ctx context.Context, req RemoveRequest) error
}

// CommandClient 是 sandbox/workspace 命令执行端口。
type CommandClient interface {
	RunCommand(ctx context.Context, req CommandRequest) (*execmodel.Result, error)
}

// StageInputRequest 描述要上传到 sandbox job 输入目录的本地或内存文件。
type StageInputRequest struct {
	Workspace WorkspaceRef      `json:"workspace"`
	Name      string            `json:"name"`
	Content   io.Reader         `json:"-"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// StageInputResult 描述 sandbox 服务返回的输入 artifact。
type StageInputResult struct {
	Artifact execmodel.InputArtifactRef `json:"artifact"`
}

// SessionOptions 描述长会话 sandbox 打开参数。
type SessionOptions struct {
	Workspace WorkspaceRef             `json:"workspace"`
	Sandbox   execmodel.SandboxProfile `json:"sandbox"`
	Options   execcontract.RunOptions  `json:"options"`
}

// SandboxSession 是多 job 共享同一 /workspace 根目录的长会话端口。
type SandboxSession interface {
	StageInput(ctx context.Context, req StageInputRequest) (*StageInputResult, error)
	Run(ctx context.Context, req CommandRequest) (*execmodel.Result, error)
	Close(ctx context.Context) error
}

// SessionClient 打开长会话 sandbox。
type SessionClient interface {
	OpenSession(ctx context.Context, opts SessionOptions) (SandboxSession, error)
}
