// Package service 提供命令执行能力的产品无关编排。
package service

import (
	"context"
	"fmt"
	"os"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/capabilities/execution/pathcontract"
)

const sandboxFallbackWarning = "sandbox runner unavailable; fell back to local execution"

// Runner 根据 SandboxProfile 在 direct runner 和 sandbox runner 之间做最小编排。
type Runner struct {
	direct        execcontract.CommandRunner
	sandbox       execcontract.SandboxRunner
	pathValidator commandPathValidator
}

type commandPathValidator interface {
	ValidateCommand(cmd execmodel.Command, opts execcontract.RunOptions) error
}

// RunnerOption 调整组合执行 runner 的产品级装配。
type RunnerOption func(*Runner)

// WithPathValidator 注入执行前路径契约校验器。
func WithPathValidator(validator commandPathValidator) RunnerOption {
	return func(r *Runner) {
		if validator != nil {
			r.pathValidator = validator
		}
	}
}

// NewRunner 创建组合执行 runner。direct runner 必须存在；sandbox runner 可后续由产品注入。
func NewRunner(direct execcontract.CommandRunner, sandbox execcontract.SandboxRunner, options ...RunnerOption) (*Runner, error) {
	if direct == nil {
		return nil, fmt.Errorf("CommandRunner未配置")
	}
	runner := &Runner{
		direct:        direct,
		sandbox:       sandbox,
		pathValidator: pathcontract.NewValidator(nil),
	}
	for _, option := range options {
		if option != nil {
			option(runner)
		}
	}
	return runner, nil
}

// Run 执行命令，并按 SandboxProfile 选择 direct 或 sandbox runner。
func (r *Runner) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	cmd = applyExecutionWorkspaceEnv(cmd, opts.Workspace)
	validator := r.pathValidator
	if validator == nil {
		validator = pathcontract.NewValidator(nil)
	}
	if err := validator.ValidateCommand(cmd, opts); err != nil {
		return nil, err
	}
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
		result, err := r.sandbox.RunInSandbox(ctx, cmd, opts.Sandbox, opts)
		if err != nil && execcontract.CodeOf(err) == execcontract.ErrCodeSandboxUnavailable {
			result, directErr := r.direct.Run(ctx, cmd, opts)
			ensureEnvironment(result, execmodel.EnvironmentLocal, "")
			addWarning(result, sandboxFallbackWarning+": "+err.Error())
			return result, directErr
		}
		return result, err
	case execmodel.SandboxRequired:
		if r.sandbox == nil {
			return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("SandboxRunner未配置"))
		}
		return r.sandbox.RunInSandbox(ctx, cmd, opts.Sandbox, opts)
	default:
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("未知sandbox模式: %s", mode))
	}
}

func applyExecutionWorkspaceEnv(cmd execmodel.Command, workspace execmodel.ExecutionWorkspace) execmodel.Command {
	if workspace.WorkDir == "" {
		workspace.WorkDir = cmd.Cwd
	}
	if workspace.WorkDir == "" {
		workspace.WorkDir = "."
	}
	if workspace.TmpDir == "" {
		workspace.TmpDir = os.TempDir()
	}
	env := make(map[string]string, len(cmd.Env)+5)
	for k, v := range cmd.Env {
		env[k] = v
	}
	setIfNonEmpty(env, "WORK_DIR", workspace.WorkDir)
	setIfNonEmpty(env, "INPUT_DIR", workspace.InputDir)
	setIfNonEmpty(env, "OUTPUT_DIR", workspace.OutputDir)
	setIfNonEmpty(env, "TMPDIR", workspace.TmpDir)
	setIfNonEmpty(env, "GENESIS_WORKSPACE", workspace.WorkDir)
	cmd.Env = env
	return cmd
}

func setIfNonEmpty(env map[string]string, key, value string) {
	if value != "" {
		env[key] = value
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
