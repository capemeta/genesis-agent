// Package service 提供命令执行能力的产品无关编排。
package service

import (
	"context"
	"fmt"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

const sandboxFallbackWarning = "sandbox runner unavailable; fell back to local execution"

// Runner 根据 SandboxProfile 在 direct runner 和 sandbox runner 之间做最小编排。
type Runner struct {
	direct  execcontract.CommandRunner
	sandbox execcontract.SandboxRunner
}

// NewRunner 创建组合执行 runner。direct runner 必须存在；sandbox runner 可后续由产品注入。
func NewRunner(direct execcontract.CommandRunner, sandbox execcontract.SandboxRunner) (*Runner, error) {
	if direct == nil {
		return nil, fmt.Errorf("CommandRunner未配置")
	}
	return &Runner{direct: direct, sandbox: sandbox}, nil
}

// Run 执行命令，并按 SandboxProfile 选择 direct 或 sandbox runner。
func (r *Runner) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	mode := opts.Sandbox.Mode
	if mode == "" {
		mode = execmodel.SandboxDisabled
	}
	switch mode {
	case execmodel.SandboxDisabled:
		result, err := r.direct.Run(ctx, cmd, opts)
		ensureEnvironment(result, execmodel.EnvironmentLocal, "")
		return result, err
	case execmodel.SandboxOptional:
		if r.sandbox == nil {
			result, err := r.direct.Run(ctx, cmd, opts)
			ensureEnvironment(result, execmodel.EnvironmentLocal, "")
			addWarning(result, sandboxFallbackWarning)
			return result, err
		}
		return r.sandbox.RunInSandbox(ctx, cmd, opts.Sandbox, opts)
	case execmodel.SandboxRequired:
		if r.sandbox == nil {
			return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("SandboxRunner未配置"))
		}
		return r.sandbox.RunInSandbox(ctx, cmd, opts.Sandbox, opts)
	default:
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("未知sandbox模式: %s", mode))
	}
}

func ensureEnvironment(result *execmodel.Result, env execmodel.ExecutionEnvironment, provider string) {
	if result == nil {
		return
	}
	if result.Environment == "" {
		result.Environment = env
	}
	if result.SandboxProvider == "" {
		result.SandboxProvider = provider
	}
}

func addWarning(result *execmodel.Result, warning string) {
	if result == nil || warning == "" {
		return
	}
	result.Warnings = append(result.Warnings, warning)
}
