// Package service 提供命令执行能力的产品无关编排。
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/capabilities/execution/pathcontract"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/platform/logger/correl"
	"genesis-agent/internal/runtime/progress"
)

const sandboxFallbackWarning = "sandbox runner unavailable; fell back to local execution"

// Runner 根据 SandboxProfile 在 direct runner 和 sandbox runner 之间做最小编排。
type Runner struct {
	direct        execcontract.CommandRunner
	sandbox       execcontract.SandboxRunner
	pathValidator commandPathValidator
	logger        logger.Logger
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

// WithLogger 注入日志记录器。
func WithLogger(l logger.Logger) RunnerOption {
	return func(r *Runner) {
		if l != nil {
			r.logger = l
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
		logger:        logger.NewNop(),
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

	l := correl.AttachLogger(ctx, r.logger).With("command", cmd.Command, "cwd", cmd.Cwd, "mode", string(mode))

	switch mode {
	case execmodel.SandboxDisabled:
		l.Info("准备在[本地宿主机]执行命令")
		progress.Emit(ctx, progress.Event{
			Kind:      progress.KindSandbox,
			Phase:     progress.PhaseStart,
			Component: "execution-runner",
			Name:      "local_host",
			Summary:   "本地宿主机直接执行 (沙箱未启用)",
		})
		result, err := r.direct.Run(ctx, cmd, opts)
		ensureEnvironment(result, execmodel.EnvironmentLocal, "")
		if err != nil {
			l.Error("本地宿主机命令执行失败", "error", err)
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseError,
				Level:     progress.LevelError,
				Component: "execution-runner",
				Name:      "local_host",
				Summary:   "本地宿主机执行失败",
				Detail:    err.Error(),
			})
		} else {
			l.Info("本地宿主机命令执行完成", "exit_code", result.ExitCode)
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseComplete,
				Component: "execution-runner",
				Name:      "local_host",
				Summary:   "本地宿主机执行结束",
			})
		}
		return result, err
	case execmodel.SandboxOptional:
		if r.sandbox == nil {
			l.Info("准备在[本地宿主机]执行命令 (未配置沙箱，降级到本地)")
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseError,
				Level:     progress.LevelWarn,
				Component: "execution-runner",
				Name:      "local_host",
				Summary:   "自动降级：本地宿主机直接执行 (沙箱未配置)",
			})
			result, err := r.direct.Run(ctx, cmd, opts)
			ensureEnvironment(result, execmodel.EnvironmentLocal, "")
			addWarning(result, sandboxFallbackWarning)
			if err != nil {
				l.Error("本地宿主机命令执行失败", "error", err)
			} else {
				l.Info("本地宿主机命令执行完成", "exit_code", result.ExitCode)
			}
			return result, err
		}
		l.Info("准备在[沙箱环境]执行命令 (可选沙箱)", "provider", opts.Sandbox.Provider, "sandbox_type", resolveSandboxType(opts.Sandbox.Provider), "runtime_profile", string(opts.Sandbox.RuntimeProfile))
		result, err := r.sandbox.RunInSandbox(ctx, cmd, opts.Sandbox, opts)
		if err != nil && (execcontract.CodeOf(err) == execcontract.ErrCodeSandboxUnavailable || execcontract.CodeOf(err) == execcontract.ErrCodeSandboxPolicyUnsupported) {
			l.Warn("沙箱服务不可用或不支持当前策略，自动降级至本地宿主机执行", "error", err)
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseError,
				Level:     progress.LevelWarn,
				Component: "execution-runner",
				Name:      "local_host",
				Summary:   "自动降级：本地宿主机直接执行 (沙箱策略未就绪)",
				Detail:    err.Error(),
			})
			result, directErr := r.direct.Run(ctx, cmd, opts)
			ensureEnvironment(result, execmodel.EnvironmentLocal, "")
			addWarning(result, sandboxFallbackWarning+": "+err.Error())
			if directErr != nil {
				l.Error("降级本地执行命令失败", "error", directErr)
			} else {
				l.Info("降级本地执行命令完成", "exit_code", result.ExitCode)
			}
			return result, directErr
		}
		if err != nil {
			l.Error("沙箱命令执行失败", "error", err)
		} else {
			l.Info("沙箱命令执行完成", "exit_code", result.ExitCode)
		}
		return result, err
	case execmodel.SandboxRequired:
		if r.sandbox == nil {
			l.Error("沙箱必填但未配置SandboxRunner，执行中止")
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseError,
				Level:     progress.LevelError,
				Component: "execution-runner",
				Name:      "local_host",
				Summary:   "安全隔离阻断：未配置沙箱执行器",
			})
			return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("SandboxRunner未配置"))
		}
		l.Info("准备在[沙箱环境]执行命令 (强制沙箱)", "provider", opts.Sandbox.Provider, "sandbox_type", resolveSandboxType(opts.Sandbox.Provider), "runtime_profile", string(opts.Sandbox.RuntimeProfile))
		result, err := r.sandbox.RunInSandbox(ctx, cmd, opts.Sandbox, opts)
		if err != nil {
			l.Error("沙箱命令执行失败", "error", err)
		} else {
			l.Info("沙箱命令执行完成", "exit_code", result.ExitCode)
		}
		return result, err
	default:
		l.Error("未知沙箱模式，执行中止")
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
	setIfNonEmpty(env, "SKILL_DIR", workspace.SkillDir)
	setIfNonEmpty(env, "GENESIS_WORKSPACE", workspace.WorkDir)
	if workspace.SkillDir != "" {
		scripts := filepath.Join(workspace.SkillDir, "scripts")
		if existing := env["PYTHONPATH"]; existing == "" {
			env["PYTHONPATH"] = scripts
		} else if !strings.Contains(existing, scripts) {
			env["PYTHONPATH"] = scripts + string(os.PathListSeparator) + existing
		}
	}
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
	result.Environment = env
	result.SandboxProvider = provider
}

func addWarning(result *execmodel.Result, warning string) {
	if result == nil || warning == "" {
		return
	}
	result.Warnings = append(result.Warnings, warning)
}

func resolveSandboxType(provider string) string {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "local" || provider == "local_host" || provider == "host" || provider == "" {
		return "本地平台沙箱 (bwrap/landlock/seatbelt)"
	}
	return "沙箱容器API (genesis-sandbox)"
}
