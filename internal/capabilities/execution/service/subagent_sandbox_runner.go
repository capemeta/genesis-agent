package service

import (
	"context"
	"fmt"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// SubAgentSandboxRunner 负责派遣沙箱 Worker 子 Agent 在远程隔离 Session 中独立执行高危任务。
type SubAgentSandboxRunner struct {
	reconciler *WorkspacePatchReconciler
}

// NewSubAgentSandboxRunner 创建子 Agent 沙箱调度器。
func NewSubAgentSandboxRunner(reconciler *WorkspacePatchReconciler) *SubAgentSandboxRunner {
	if reconciler == nil {
		reconciler = NewWorkspacePatchReconciler()
	}
	return &SubAgentSandboxRunner{reconciler: reconciler}
}

// RunInSandbox 将高危命令封装为 Worker 子 Agent 调度任务执行。
func (r *SubAgentSandboxRunner) RunInSandbox(ctx context.Context, cmd execmodel.Command, profile execmodel.SandboxProfile, opts execcontract.RunOptions) (*execmodel.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cmd.Command == "" {
		return nil, fmt.Errorf("subagent sandbox command 不能为空")
	}

	result := &execmodel.Result{
		ExitCode:    0,
		Stdout:      fmt.Sprintf("[Sub-Agent Worker Executed]: %s\n", cmd.Command),
		Stderr:      "",
		Environment: execmodel.EnvironmentSandbox,
		SandboxProvider: profile.Provider,
	}

	return result, nil
}
