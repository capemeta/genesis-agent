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

// Runner 根据 SandboxProfile 在 direct runner 和 sandbox runner 之间做最小编排。
type Runner struct {
	direct        execcontract.CommandRunner
	sandbox       execcontract.SandboxRunner
	localSandbox  execcontract.SandboxRunner
	pathValidator commandPathValidator
	astAnalyzer   *ScriptASTAnalyzer
	logger        logger.Logger
}

type commandPathValidator interface {
	ValidateCommand(cmd execmodel.Command, opts execcontract.RunOptions) error
}

// RunnerOption 调整组合执行 runner 的产品级装配。
type RunnerOption func(*Runner)

// WithLocalSandbox 注入专用的本地平台沙箱 Runner (如进程隔离 AppContainer/bwrap/seatbelt)。
func WithLocalSandbox(localRunner execcontract.SandboxRunner) RunnerOption {
	return func(r *Runner) {
		if localRunner != nil {
			r.localSandbox = localRunner
		}
	}
}

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
		astAnalyzer:   NewScriptASTAnalyzer(),
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
	if err := validateExecutionBinding(opts); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cmd.Cwd) == "" {
		cmd.Cwd = opts.Workspace.WorkDir
	}
	if !filepath.IsAbs(cmd.Cwd) {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("cmd.Cwd 必须为物理绝对路径, 收到: %q", cmd.Cwd))
	}
	cmd = applyExecutionWorkspaceEnv(cmd, opts.Workspace)
	cmd.Env = NewEnvSanitizer().SanitizeEnv(cmd.Env)
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

	// 触发复合脚本 AST 分析：若脚本内部含有敏感命令，强切为 SandboxRequired
	if r.astAnalyzer != nil && r.astAnalyzer.AnalyzeScript(cmd.Command) == RiskLevelUntrustedRemote {
		if mode == execmodel.SandboxDisabled {
			mode = execmodel.SandboxRequired
		}
	}

	l := correl.AttachLogger(ctx, r.logger).With("command", cmd.Command, "cwd", cmd.Cwd, "mode", string(mode))

	cmdStr := strings.TrimSpace(cmd.Command)

	switch mode {
	case execmodel.SandboxDisabled:
		startSummary := "本地宿主机直接执行 (沙箱未启用)"
		if cmdStr != "" {
			startSummary = fmt.Sprintf("准备在[本地宿主机]执行命令: %s (沙箱未启用)", cmdStr)
		}
		l.Info("准备在[本地宿主机]执行命令")
		progress.Emit(ctx, progress.Event{
			Kind:      progress.KindSandbox,
			Phase:     progress.PhaseStart,
			Component: "execution-runner",
			Name:      "local_host",
			Summary:   startSummary,
			Detail:    cmdStr,
		})
		result, err := r.direct.Run(ctx, cmd, opts)
		ensureEnvironment(result, execmodel.EnvironmentLocal, "")
		if err != nil {
			l.Error("本地宿主机命令执行失败", "error", err)
			failSummary := "本地宿主机执行失败"
			if cmdStr != "" {
				failSummary = fmt.Sprintf("本地宿主机执行失败: %s", cmdStr)
			}
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseError,
				Level:     progress.LevelError,
				Component: "execution-runner",
				Name:      "local_host",
				Summary:   failSummary,
				Detail:    err.Error(),
			})
		} else {
			l.Info("本地宿主机命令执行完成", "exit_code", result.ExitCode)
			completeSummary := "本地宿主机执行结束"
			if cmdStr != "" {
				completeSummary = fmt.Sprintf("本地宿主机执行完成: %s", cmdStr)
			}
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseComplete,
				Component: "execution-runner",
				Name:      "local_host",
				Summary:   completeSummary,
			})
		}
		return result, err
	case execmodel.SandboxOptional:
		if isLocalSandboxProvider(opts.Sandbox.Provider) {
			return r.runLocalSandbox(ctx, cmd, opts, "可选沙箱", l)
		}
		if r.sandbox == nil {
			l.Warn("可选沙箱未配置；返回控制面，由 Harness 决定是否创建独立 Host attempt")
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseError,
				Level:     progress.LevelWarn,
				Component: "execution-runner",
				Name:      "sandbox",
				Summary:   "沙箱不可用，等待 Harness 决策",
			})
			return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("SandboxRunner未配置；Harness 必须创建独立 backend attempt 后重试"))
		}
		startSummary := fmt.Sprintf("准备在[远程沙箱环境]执行命令 (%s)", "可选沙箱")
		if cmdStr != "" {
			startSummary = fmt.Sprintf("准备在[远程沙箱环境]执行命令: %s (可选沙箱)", cmdStr)
		}
		l.Info(startSummary, "provider", opts.Sandbox.Provider, "sandbox_type", resolveSandboxType(opts.Sandbox.Provider), "runtime_profile", string(opts.Sandbox.RuntimeProfile))
		result, err := r.sandbox.RunInSandbox(ctx, cmd, opts.Sandbox, opts)
		if err != nil && (execcontract.CodeOf(err) == execcontract.ErrCodeSandboxUnavailable || execcontract.CodeOf(err) == execcontract.ErrCodeSandboxPolicyUnsupported) {
			l.Warn("沙箱服务不可用或策略不支持；返回控制面，由 Harness 决定独立 attempt", "error", err)
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseError,
				Level:     progress.LevelWarn,
				Component: "execution-runner",
				Name:      "sandbox",
				Summary:   "沙箱不可用，等待 Harness 决策",
				Detail:    err.Error(),
			})
			return nil, err
		}
		if err != nil {
			l.Error("沙箱命令执行失败", "error", err)
		} else {
			l.Info("沙箱命令执行完成", "exit_code", result.ExitCode)
		}
		return result, err
	case execmodel.SandboxRequired:
		if isLocalSandboxProvider(opts.Sandbox.Provider) {
			return r.runLocalSandbox(ctx, cmd, opts, "强制沙箱", l)
		}
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
		startSummary := fmt.Sprintf("准备在[远程沙箱环境]执行命令 (%s)", "强制沙箱")
		if cmdStr != "" {
			startSummary = fmt.Sprintf("准备在[远程沙箱环境]执行命令: %s (强制沙箱)", cmdStr)
		}
		l.Info(startSummary, "provider", opts.Sandbox.Provider, "sandbox_type", resolveSandboxType(opts.Sandbox.Provider), "runtime_profile", string(opts.Sandbox.RuntimeProfile))
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

func (r *Runner) runLocalSandbox(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions, modeStr string, l logger.Logger) (*execmodel.Result, error) {
	cmdStr := strings.TrimSpace(cmd.Command)
	startSummary := fmt.Sprintf("准备在[本地平台沙箱]执行命令 (%s)", modeStr)
	if cmdStr != "" {
		startSummary = fmt.Sprintf("准备在[本地平台沙箱]执行命令: %s (%s)", cmdStr, modeStr)
	}
	l.Info(startSummary, "provider", opts.Sandbox.Provider, "sandbox_type", resolveSandboxType(opts.Sandbox.Provider), "runtime_profile", string(opts.Sandbox.RuntimeProfile))
	progress.Emit(ctx, progress.Event{
		Kind:      progress.KindSandbox,
		Phase:     progress.PhaseStart,
		Component: "execution-runner",
		Name:      opts.Sandbox.Provider,
		Summary:   startSummary,
		Detail:    fmt.Sprintf("command=%s provider=%s sandbox_type=%s", cmdStr, opts.Sandbox.Provider, resolveSandboxType(opts.Sandbox.Provider)),
	})
	var result *execmodel.Result
	var err error
	if r.localSandbox != nil {
		result, err = r.localSandbox.RunInSandbox(ctx, cmd, opts.Sandbox, opts)
	} else {
		result, err = r.direct.Run(ctx, cmd, opts)
		ensureEnvironment(result, execmodel.EnvironmentLocal, opts.Sandbox.Provider)
	}
	if err != nil {
		l.Error("本地平台沙箱命令执行失败", "error", err)
		failSummary := "本地平台沙箱执行失败"
		if cmdStr != "" {
			failSummary = fmt.Sprintf("本地平台沙箱执行失败: %s", cmdStr)
		}
		progress.Emit(ctx, progress.Event{
			Kind:      progress.KindSandbox,
			Phase:     progress.PhaseError,
			Level:     progress.LevelError,
			Component: "execution-runner",
			Name:      opts.Sandbox.Provider,
			Summary:   failSummary,
			Detail:    err.Error(),
		})
	} else {
		l.Info("本地平台沙箱命令执行完成", "exit_code", result.ExitCode)
		completeSummary := "本地平台沙箱执行完成"
		if cmdStr != "" {
			completeSummary = fmt.Sprintf("本地平台沙箱执行完成: %s", cmdStr)
		}
		progress.Emit(ctx, progress.Event{
			Kind:      progress.KindSandbox,
			Phase:     progress.PhaseComplete,
			Component: "execution-runner",
			Name:      opts.Sandbox.Provider,
			Summary:   completeSummary,
		})
	}
	return result, err
}

func isLocalSandboxProvider(provider string) bool {
	kind := execmodel.ResolveBackendKind(provider)
	return kind == execmodel.BackendKindLocalSandbox || kind == execmodel.BackendKindHost
}

func applyExecutionWorkspaceEnv(cmd execmodel.Command, workspace execmodel.ExecutionWorkspace) execmodel.Command {
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

func validateExecutionBinding(opts execcontract.RunOptions) error {
	if strings.TrimSpace(opts.Binding.ID) == "" || strings.TrimSpace(opts.Binding.Owner.RunID) == "" {
		return execcontract.NewError(execcontract.ErrCodeExecutionBindingRequired, fmt.Errorf("执行命令缺少可信 ExecutionBinding"))
	}
	switch opts.Binding.Mode {
	case execmodel.WorkspaceModeProject, execmodel.WorkspaceModeTask, execmodel.WorkspaceModeSession:
	default:
		return execcontract.NewError(execcontract.ErrCodeWorkspaceModeNotAllowed, fmt.Errorf("不支持的 workspace mode: %q", opts.Binding.Mode))
	}
	if err := opts.Binding.Validate(); err != nil {
		return execcontract.NewError(execcontract.ErrCodeExecutionBindingConflict, fmt.Errorf("ExecutionBinding 无效: %w", err))
	}
	if err := opts.Workspace.ValidateFor(opts.Binding); err != nil {
		return execcontract.NewError(execcontract.ErrCodeExecutionBindingConflict, fmt.Errorf("ExecutionWorkspace 与 binding 不一致: %w", err))
	}
	return nil
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
	switch execmodel.ResolveBackendKind(provider) {
	case execmodel.BackendKindHost:
		return "宿主环境直跑 (local_host)"
	case execmodel.BackendKindLocalSandbox:
		return "本地平台沙箱 (ProcessConstrained/AppContainer/bwrap/seatbelt)"
	default:
		return "沙箱容器API (genesis-sandbox)"
	}
}
