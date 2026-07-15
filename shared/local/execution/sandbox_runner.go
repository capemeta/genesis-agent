package execution

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/runtime/progress"
	localsandbox "genesis-agent/shared/local/sandbox"
	"genesis-agent/shared/local/sandbox/seatbelt"
)

// SandboxRunnerOptions 控制本地平台沙箱 runner。
type SandboxRunnerOptions struct {
	Manager        *localsandbox.Manager
	WorkspaceRoots []string
	WritableRoots  []string
}

type argvRunner interface {
	RunArgv(ctx context.Context, command ArgvCommand, opts execcontract.RunOptions) (*execmodel.Result, error)
}

type processConstrainedRunner interface {
	RunArgvProcessConstrained(ctx context.Context, command ArgvCommand, opts execcontract.RunOptions) (*execmodel.Result, error)
}

// SandboxRunner 将 shared/local/sandbox plan 适配为 execution.SandboxRunner。
type SandboxRunner struct {
	manager        *localsandbox.Manager
	runner         argvRunner
	workspaceRoots []string
	writableRoots  []string
}

// NewSandboxRunner 创建本地平台 SandboxRunner。
func NewSandboxRunner(runner *Runner, opts SandboxRunnerOptions) (*SandboxRunner, error) {
	if runner == nil {
		return nil, fmt.Errorf("本地argv runner未配置")
	}
	return newSandboxRunner(runner, opts), nil
}

func newSandboxRunner(runner argvRunner, opts SandboxRunnerOptions) *SandboxRunner {
	manager := opts.Manager
	if manager == nil {
		manager = localsandbox.NewManager()
	}
	return &SandboxRunner{manager: manager, runner: runner, workspaceRoots: append([]string{}, opts.WorkspaceRoots...), writableRoots: append([]string{}, opts.WritableRoots...)}
}

// RunInSandbox 在本地平台沙箱中执行命令。Optional 降级由 sandbox.Manager 返回 degraded plan。
func (r *SandboxRunner) RunInSandbox(ctx context.Context, cmd execmodel.Command, profile execmodel.SandboxProfile, opts execcontract.RunOptions) (*execmodel.Result, error) {
	if strings.TrimSpace(cmd.Command) == "" {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("command不能为空"))
	}
	argv, shell, err := ShellArgv(cmd.Shell, cmd.Command)
	if err != nil {
		return nil, err
	}
	preference := preferenceFromMode(profile.Mode)
	localProfile := localSandboxProfile(cmd, profile)
	workspaceRoots := append([]string{}, r.workspaceRoots...)
	if cmd.Cwd != "" {
		workspaceRoots = append(workspaceRoots, cmd.Cwd)
	}
	writableRoots := append([]string{}, r.writableRoots...)
	writableRoots = append(writableRoots, localProfile.FileSystem.WritableRoots...)
	plan, err := r.manager.BuildPlan(ctx, localsandbox.BuildRequest{
		Preference:       preference,
		Command:          localsandbox.CommandSpec{Argv: argv, Env: cmd.Env, Cwd: cmd.Cwd},
		Profile:          localProfile,
		SandboxPolicyCwd: cmd.Cwd,
		WorkspaceRoots:   workspaceRoots,
		Writables:        writableRoots,
	})
	if err != nil {
		return nil, mapSandboxError(err)
	}
	argvCommand := ArgvCommand{Argv: plan.Command.Argv, Env: plan.Command.Env, Cwd: plan.Command.Cwd, Stdin: cmd.Stdin, DisplayCommand: cmd.Command, Shell: shell}

	isSandbox := plan.Type != localsandbox.TypeNone
	if isSandbox {
		progress.Emit(ctx, progress.Event{
			Kind:      progress.KindSandbox,
			Phase:     progress.PhaseStart,
			Component: "local-platform-sandbox",
			Name:      string(plan.Type),
			Summary:   "启动本地平台沙箱",
			Detail:    fmt.Sprintf("沙箱类型: %s, 级别: %s, 隔离等级: %s", plan.Type, plan.WindowsLevel, plan.Enforcement),
		})
	} else if profile.Mode != execmodel.SandboxDisabled {
		summary := "自动降级：本地宿主机直接执行 (沙箱未配置)"
		if plan.Degraded {
			summary = "自动降级：本地宿主机直接执行 (沙箱策略未就绪)"
		}
		progress.Emit(ctx, progress.Event{
			Kind:      progress.KindSandbox,
			Phase:     progress.PhaseError,
			Level:     progress.LevelWarn,
			Component: "local-platform-sandbox",
			Name:      "local_host",
			Summary:   summary,
			Detail:    strings.Join(plan.Warnings, "; "),
		})
	}

	result, err := r.runPlannedCommand(ctx, plan, argvCommand, opts)

	if isSandbox {
		if err != nil {
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseError,
				Level:     progress.LevelError,
				Component: "local-platform-sandbox",
				Name:      string(plan.Type),
				Summary:   "本地平台沙箱执行异常",
				Detail:    err.Error(),
			})
		} else {
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseComplete,
				Component: "local-platform-sandbox",
				Name:      string(plan.Type),
				Summary:   "本地平台沙箱执行结束",
			})
		}
	} else if profile.Mode != execmodel.SandboxDisabled {
		if err != nil {
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseError,
				Level:     progress.LevelError,
				Component: "local-platform-sandbox",
				Name:      "local_host",
				Summary:   "宿主环境直接执行异常",
				Detail:    err.Error(),
			})
		} else {
			progress.Emit(ctx, progress.Event{
				Kind:      progress.KindSandbox,
				Phase:     progress.PhaseComplete,
				Component: "local-platform-sandbox",
				Name:      "local_host",
				Summary:   "宿主环境直接执行完成",
			})
		}
	}

	if result != nil {
		if plan.Type == localsandbox.TypeNone {
			result.Environment = execmodel.EnvironmentLocal
			result.SandboxProvider = ""
		} else {
			result.Environment = execmodel.EnvironmentSandbox
			result.SandboxProvider = providerName(profile, plan.Type)
		}
		result.Warnings = append(result.Warnings, plan.Warnings...)

		// macOS Seatbelt denial 解析：提取结构化拒绝事件
		if plan.Type == localsandbox.TypeMacOSSeatbelt && seatbelt.HasDenial(result.Stderr) {
			violations := seatbelt.ParseDenials(result.Stderr)
			for _, v := range violations {
				entry := v.Operation
				if v.Path != "" {
					entry += ":" + v.Path
				}
				result.SandboxViolations = append(result.SandboxViolations, entry)
			}
		}
	}
	return result, err
}

func (r *SandboxRunner) runPlannedCommand(ctx context.Context, plan *localsandbox.Plan, command ArgvCommand, opts execcontract.RunOptions) (*execmodel.Result, error) {
	if plan != nil && plan.Type == localsandbox.TypeWindowsProcessConstrained {
		if constrained, ok := r.runner.(processConstrainedRunner); ok {
			return constrained.RunArgvProcessConstrained(ctx, command, opts)
		}
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("Windows process constrained runner未配置"))
	}
	return r.runner.RunArgv(ctx, command, opts)
}

func preferenceFromMode(mode execmodel.SandboxMode) localsandbox.Preference {
	switch mode {
	case execmodel.SandboxRequired:
		return localsandbox.PreferenceRequired
	case execmodel.SandboxOptional:
		return localsandbox.PreferenceAuto
	case execmodel.SandboxDisabled, "":
		return localsandbox.PreferenceDisabled
	default:
		return localsandbox.PreferenceAuto
	}
}

func localSandboxProfile(cmd execmodel.Command, profile execmodel.SandboxProfile) localsandbox.Profile {
	metadata := profile.Metadata
	cwd := cmd.Cwd
	fsMode := metadataValue(metadata, "filesystem", "workspace_write")
	network := localsandbox.NetworkMode(metadataValue(metadata, "network", string(localsandbox.NetworkDisabled)))
	if network == "" {
		network = localsandbox.NetworkDisabled
	}
	fs := localsandbox.FileSystemPolicy{AllowFullDiskRead: true}
	switch fsMode {
	case "full_access", "danger_full_access":
		fs.AllowFullDiskWrite = true
	case "read_only":
		fs.AllowFullDiskWrite = false
	case "workspace_write", "":
		fs.AllowFullDiskWrite = false
		if cwd != "" {
			fs.WritableRoots = []string{cwd}
		}
	default:
		fs.AllowFullDiskWrite = false
		if cwd != "" {
			fs.WritableRoots = []string{cwd}
		}
	}
	fs.WritableRoots = append(fs.WritableRoots, splitMetadataPaths(metadataValue(metadata, "writable_roots", ""))...)
	fs.ReadableRoots = append(fs.ReadableRoots, splitMetadataPaths(metadataValue(metadata, "readable_roots", ""))...)
	return localsandbox.Profile{FileSystem: fs, Network: network, Process: localsandbox.ProcessPolicy{KillProcessTree: true, ConstrainToken: runtime.GOOS == "windows"}}
}

func metadataValue(metadata map[string]string, key, fallback string) string {
	if metadata == nil {
		return fallback
	}
	if value := strings.TrimSpace(metadata[key]); value != "" {
		return value
	}
	return fallback
}

func splitMetadataPaths(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '|' || r == ',' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if abs, err := filepath.Abs(part); err == nil {
			part = abs
		}
		out = append(out, part)
	}
	return out
}

func providerName(profile execmodel.SandboxProfile, typ localsandbox.Type) string {
	if profile.Provider != "" {
		return profile.Provider
	}
	if typ == "" {
		return "local-platform"
	}
	return string(typ)
}

func mapSandboxError(err error) error {
	switch localsandbox.CodeOf(err) {
	case localsandbox.ErrCodeInvalidInput:
		return execcontract.NewError(execcontract.ErrCodeInvalidInput, err)
	case localsandbox.ErrCodeSandboxUnavailable:
		return execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, err)
	case localsandbox.ErrCodePolicyUnsupported:
		return execcontract.NewError(execcontract.ErrCodeSandboxPolicyUnsupported, err)
	default:
		return execcontract.NewError(execcontract.ErrCodeRunnerFailed, err)
	}
}
