package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
)

// Session 是 agent 内部使用的 session-backed sandbox helper。
type Session struct {
	raw       sandboxcontract.SandboxSession
	files     sandboxcontract.FileSystemClient
	workspace sandboxcontract.WorkspaceRef
	sandbox   execmodel.SandboxProfile
	run       execcontract.RunOptions

	closeOnce sync.Once
	closeErr  error
}

// Open 打开一个可复用 /workspace 的 sandbox session。
func Open(ctx context.Context, deps Deps, opts Options) (*Session, error) {
	if deps.Sessions == nil {
		return nil, fmt.Errorf("sandbox session client未配置")
	}
	if deps.Files == nil {
		return nil, fmt.Errorf("sandbox filesystem client未配置")
	}
	opts = mergeOptions(DefaultOptions(), opts)
	if err := opts.Run.Binding.Validate(); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeExecutionBindingRequired, fmt.Errorf("打开 sandbox session 需要有效 ExecutionBinding: %w", err))
	}
	if err := opts.Run.Workspace.ValidateFor(opts.Run.Binding); err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeExecutionBindingConflict, fmt.Errorf("sandbox session workspace 与 binding 不一致: %w", err))
	}
	raw, err := deps.Sessions.OpenSession(ctx, sandboxcontract.SessionOptions{
		Workspace: opts.Workspace,
		Sandbox:   opts.Sandbox,
		Options:   opts.Run,
	})
	if err != nil {
		return nil, err
	}
	return &Session{
		raw:       raw,
		files:     deps.Files,
		workspace: raw.Workspace(),
		sandbox:   opts.Sandbox,
		run:       opts.Run,
	}, nil
}

// Workspace 返回 session scoped WorkspaceFS 引用。
func (s *Session) Workspace() sandboxcontract.WorkspaceRef {
	if s == nil {
		return sandboxcontract.WorkspaceRef{}
	}
	return s.workspace
}

// ExpiresAt 返回底层实现提供的权威 lease；不支持时返回零值并由 Harness fail closed。
func (s *Session) ExpiresAt() time.Time {
	if s == nil || s.raw == nil {
		return time.Time{}
	}
	if leased, ok := s.raw.(interface{ ExpiresAt() time.Time }); ok {
		return leased.ExpiresAt()
	}
	return time.Time{}
}

// Raw 返回底层 session 端口。
func (s *Session) Raw() sandboxcontract.SandboxSession {
	if s == nil {
		return nil
	}
	return s.raw
}

// Run 执行一条命令，并复用当前 session 的 /workspace。
func (s *Session) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	if s == nil || s.raw == nil {
		return nil, fmt.Errorf("sandbox session未打开")
	}
	opts = mergeRunOptions(s.run, opts)
	return s.raw.Run(ctx, sandboxcontract.CommandRequest{
		Workspace: s.workspace,
		Command:   cmd,
		Sandbox:   s.sandbox,
		Options:   opts,
	})
}

// RunCommand 用 sh 执行 verbatim 命令字符串。
func (s *Session) RunCommand(ctx context.Context, command string, opts execcontract.RunOptions) (*execmodel.Result, error) {
	return s.Run(ctx, execmodel.Command{Command: command, Cwd: "/workspace", Shell: execmodel.ShellSh}, opts)
}

// WriteFile 写入 session workspace 相对路径。
func (s *Session) WriteFile(ctx context.Context, path string, content []byte, opts fscontract.WriteOptions) error {
	if s == nil || s.files == nil {
		return fmt.Errorf("sandbox filesystem client未配置")
	}
	return s.files.WriteFile(ctx, sandboxcontract.WriteFileRequest{
		Workspace: s.workspace,
		Path:      resolvedWorkspacePath(path),
		Content:   content,
		Options:   opts,
	})
}

// ReadFile 读取 session workspace 相对路径。
func (s *Session) ReadFile(ctx context.Context, path string, opts fscontract.ReadOptions) ([]byte, error) {
	if s == nil || s.files == nil {
		return nil, fmt.Errorf("sandbox filesystem client未配置")
	}
	return s.files.ReadFile(ctx, sandboxcontract.FileRequest{
		Workspace: s.workspace,
		Path:      resolvedWorkspacePath(path),
	}, opts)
}

// ListDir 枚举 session workspace 目录。
func (s *Session) ListDir(ctx context.Context, path string, opts fscontract.ListOptions) ([]fsmodel.DirEntry, error) {
	if s == nil || s.files == nil {
		return nil, fmt.Errorf("sandbox filesystem client未配置")
	}
	return s.files.ListDir(ctx, sandboxcontract.ListDirRequest{
		Workspace: s.workspace,
		Path:      resolvedWorkspacePath(path),
		Options:   opts,
	})
}

// Walk 递归枚举 session workspace 目录。
func (s *Session) Walk(ctx context.Context, path string, opts fscontract.WalkOptions) (*fsmodel.WalkOutcome, error) {
	if s == nil || s.files == nil {
		return nil, fmt.Errorf("sandbox filesystem client未配置")
	}
	return s.files.Walk(ctx, sandboxcontract.WalkRequest{
		Workspace: s.workspace,
		Path:      resolvedWorkspacePath(path),
		Options:   opts,
	})
}

// Stat 返回 session workspace 文件状态。
func (s *Session) Stat(ctx context.Context, path string) (*fsmodel.FileStat, error) {
	if s == nil || s.files == nil {
		return nil, fmt.Errorf("sandbox filesystem client未配置")
	}
	return s.files.Stat(ctx, sandboxcontract.FileRequest{
		Workspace: s.workspace,
		Path:      resolvedWorkspacePath(path),
	})
}

// MkdirAll 创建 session workspace 目录。
func (s *Session) MkdirAll(ctx context.Context, path string, opts fscontract.MkdirOptions) error {
	if s == nil || s.files == nil {
		return fmt.Errorf("sandbox filesystem client未配置")
	}
	return s.files.MkdirAll(ctx, sandboxcontract.MkdirRequest{
		Workspace: s.workspace,
		Path:      resolvedWorkspacePath(path),
		Options:   opts,
	})
}

// Remove 删除 session workspace 文件或目录。
func (s *Session) Remove(ctx context.Context, path string, opts fscontract.RemoveOptions) error {
	if s == nil || s.files == nil {
		return fmt.Errorf("sandbox filesystem client未配置")
	}
	return s.files.Remove(ctx, sandboxcontract.RemoveRequest{
		Workspace: s.workspace,
		Path:      resolvedWorkspacePath(path),
		Options:   opts,
	})
}

// Close 关闭底层 sandbox session。
func (s *Session) Close(ctx context.Context) error {
	if s == nil || s.raw == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closeErr = s.raw.Close(ctx)
	})
	return s.closeErr
}

func resolvedWorkspacePath(path string) fsmodel.ResolvedPath {
	return fsmodel.ResolvedPath{
		DisplayPath:  path,
		WorkspaceRel: path,
		RawPath:      path,
		Scope:        fsmodel.PathScopeWorkspace,
	}
}

func mergeOptions(base, override Options) Options {
	out := base
	if override.Workspace.ID != "" || override.Workspace.Provider != "" || len(override.Workspace.Metadata) > 0 {
		out.Workspace = override.Workspace
	}
	out.Sandbox = mergeSandboxProfile(out.Sandbox, override.Sandbox)
	out.Run = mergeRunOptions(out.Run, override.Run)
	return out
}

func mergeSandboxProfile(base, override execmodel.SandboxProfile) execmodel.SandboxProfile {
	out := base
	if override.Mode != "" {
		out.Mode = override.Mode
	}
	if override.Provider != "" {
		out.Provider = override.Provider
	}
	if override.WorkspaceID != "" {
		out.WorkspaceID = override.WorkspaceID
	}
	if override.RuntimeProfile != "" {
		out.RuntimeProfile = override.RuntimeProfile
	}
	if override.TaskType != "" {
		out.TaskType = override.TaskType
	}
	if override.Operation != "" {
		out.Operation = override.Operation
	}
	if override.Language != "" {
		out.Language = override.Language
	}
	if override.RiskLevel != "" {
		out.RiskLevel = override.RiskLevel
	}
	if len(override.Metadata) > 0 {
		out.Metadata = override.Metadata
	}
	return out
}

func mergeRunOptions(base, override execcontract.RunOptions) execcontract.RunOptions {
	out := base
	if override.Timeout > 0 {
		out.Timeout = override.Timeout
	}
	if override.MaxOutputBytes > 0 {
		out.MaxOutputBytes = override.MaxOutputBytes
	}
	if override.Sandbox.Mode != "" || override.Sandbox.Provider != "" || override.Sandbox.RuntimeProfile != "" || override.Sandbox.TaskType != "" || override.Sandbox.Operation != "" {
		out.Sandbox = override.Sandbox
	}
	if override.Binding.ID != "" || override.Binding.Mode != "" || override.Binding.Owner.RunID != "" {
		out.Binding = override.Binding
	}
	out.Workspace = mergeExecutionWorkspace(out.Workspace, override.Workspace)
	if len(override.StagedInputs) > 0 {
		out.StagedInputs = override.StagedInputs
	}
	if override.OutputDiscoveryPolicy != "" {
		out.OutputDiscoveryPolicy = override.OutputDiscoveryPolicy
	}
	return out
}

func mergeExecutionWorkspace(base, override execmodel.ExecutionWorkspace) execmodel.ExecutionWorkspace {
	out := base
	if override.WorkDir != "" {
		out.WorkDir = override.WorkDir
	}
	if override.InputDir != "" {
		out.InputDir = override.InputDir
	}
	if override.OutputDir != "" {
		out.OutputDir = override.OutputDir
	}
	if override.TmpDir != "" {
		out.TmpDir = override.TmpDir
	}
	if override.SkillDir != "" {
		out.SkillDir = override.SkillDir
	}
	if len(override.Metadata) > 0 {
		out.Metadata = override.Metadata
	}
	return out
}
