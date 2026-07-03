// Package sandbox 提供基于 sandbox/workspace client 的 SandboxRunner 适配。
package sandbox

import (
	"context"
	"fmt"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
)

// Runner 将 sandbox CommandClient 适配为 execution.SandboxRunner。
type Runner struct {
	client    sandboxcontract.CommandClient
	workspace sandboxcontract.WorkspaceRef
}

// NewRunner 创建 sandbox 命令 runner。
func NewRunner(client sandboxcontract.CommandClient, workspace sandboxcontract.WorkspaceRef) (*Runner, error) {
	if client == nil {
		return nil, fmt.Errorf("sandbox CommandClient未配置")
	}
	if workspace.ID == "" {
		return nil, fmt.Errorf("sandbox workspace id不能为空")
	}
	return &Runner{client: client, workspace: workspace}, nil
}

// RunInSandbox 在 sandbox/workspace 中执行命令。
func (r *Runner) RunInSandbox(ctx context.Context, cmd execmodel.Command, sandbox execmodel.SandboxProfile, opts execcontract.RunOptions) (*execmodel.Result, error) {
	return r.client.RunCommand(ctx, sandboxcontract.CommandRequest{
		Workspace: r.workspace,
		Command:   cmd,
		Sandbox:   sandbox,
		Options:   opts,
	})
}
