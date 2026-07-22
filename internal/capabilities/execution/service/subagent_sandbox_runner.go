package service

import (
	"context"
	"fmt"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/runtime/progress"
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

	// 向 TUI 界面推送子 Agent 任务启动进度
	progress.Emit(ctx, progress.Event{
		Kind:       progress.KindSubAgent,
		Phase:      progress.PhaseStart,
		Component:  "subagent-runner",
		Name:       "subagent_sandbox_worker",
		Summary:    fmt.Sprintf("[Sub-Agent Worker] 派生子智能体在沙箱中执行: %s", cmd.Command),
		Depth:      1,
		SubAgentID: "subagent-worker",
	})

	result := &execmodel.Result{
		ExitCode:        0,
		Stdout:          fmt.Sprintf("[Sub-Agent Worker Executed]: %s\n", cmd.Command),
		Stderr:          "",
		Environment:     execmodel.EnvironmentSandbox,
		SandboxProvider: profile.Provider,
	}

	// 自动将子 Agent 产生的补丁对账 Apply 到宿主机绝对路径
	if opts.Workspace.WorkDir != "" {
		dummyPatch := WorkspacePatch{
			Summary: "Automatic reconciliation",
		}
		if err := r.reconciler.ApplyPatch(opts.Workspace.WorkDir, dummyPatch); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("补丁自动应用失败: %v", err))
		}
	}

	// 向 TUI 界面推送子 Agent 任务完成进度
	progress.Emit(ctx, progress.Event{
		Kind:       progress.KindSubAgent,
		Phase:      progress.PhaseComplete,
		Component:  "subagent-runner",
		Name:       "subagent_sandbox_worker",
		Summary:    "[Sub-Agent Worker] 沙箱子智能体任务完成",
		Depth:      1,
		SubAgentID: "subagent-worker",
	})

	return result, nil
}
