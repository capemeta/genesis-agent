// Package session 提供基于 sandbox 端口的长会话便利封装。
package session

import (
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
)

const (
	// DefaultTimeout 是单次 session 命令的默认执行超时。
	DefaultTimeout = 60 * time.Second
)

// Deps 是 Session helper 的端口依赖。
type Deps struct {
	Sessions sandboxcontract.SessionClient
	Files    sandboxcontract.FileSystemClient
}

// Options 描述要打开的 sandbox session。
type Options struct {
	Workspace sandboxcontract.WorkspaceRef
	Sandbox   execmodel.SandboxProfile
	Run       execcontract.RunOptions
}

// DefaultOptions 返回产品无关的 session workspace 默认目录。
func DefaultOptions() Options {
	return Options{
		Run: execcontract.RunOptions{
			Timeout: DefaultTimeout,
			Workspace: execmodel.ExecutionWorkspace{
				WorkDir:   "/workspace",
				InputDir:  "/workspace/input",
				OutputDir: "/workspace/output",
				TmpDir:    "/workspace/tmp",
			},
		},
	}
}
