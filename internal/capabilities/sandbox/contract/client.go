// Package contract 定义产品无关 sandbox/workspace 端口。
package contract

import (
	"context"
	"time"

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

// SessionOptions 描述长会话 sandbox 打开参数。
type SessionOptions struct {
	Workspace WorkspaceRef             `json:"workspace"`
	Sandbox   execmodel.SandboxProfile `json:"sandbox"`
	Options   execcontract.RunOptions  `json:"options"`
}

// SandboxSession 是多 job 共享同一 /workspace 根目录的长会话端口。
// Workspace 返回可传给 FileSystemClient 的 session scoped workspace 引用。
type SandboxSession interface {
	Workspace() WorkspaceRef
	Run(ctx context.Context, req CommandRequest) (*execmodel.Result, error)
	Close(ctx context.Context) error
}

// LeasedSandboxSession 额外暴露服务端确认的 lease 到期时间；Harness 不得自行猜测 TTL。
type LeasedSandboxSession interface {
	SandboxSession
	ExpiresAt() time.Time
}

// SessionClient 打开长会话 sandbox。
type SessionClient interface {
	OpenSession(ctx context.Context, opts SessionOptions) (SandboxSession, error)
}

// ExecutionSessionStore 持久化“逻辑执行会话 -> durable workspace”的映射。
// 实现只能保存 workspace_id 等稳定身份，禁止持久化短命的 session_id、sandbox_id 或凭据。
type ExecutionSessionStore interface {
	LoadExecutionSession(ctx context.Context, key string) (WorkspaceRef, bool, error)
	SaveExecutionSession(ctx context.Context, key string, workspace WorkspaceRef) error
	DeleteExecutionSession(ctx context.Context, key string) error
}

// RemoteSessionBinder 在远程 Session 可用后，将 execution binding 绑定到权威 Workspace 与租约。
// ProducedResource 控制面依赖该绑定生成不泄漏物理路径的远程资源定位符。
type RemoteSessionBinder interface {
	ExecutionSessionStore
	BindRemoteSession(ctx context.Context, tenantID, runID, bindingID string, workspace WorkspaceRef, expiresAt time.Time) error
}
