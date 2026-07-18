// Package command 将 Hook command handler 协议适配到 execution 能力域。
package command

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/capabilities/hook/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

// Runner 通过 execution.ExecutionRunner 执行 command handler。
type Runner struct {
	exec           execcontract.ExecutionRunner
	defaultTimeout time.Duration
}

func NewRunner(exec execcontract.ExecutionRunner, defaultTimeout time.Duration) (*Runner, error) {
	if exec == nil {
		return nil, fmt.Errorf("Hook command execution runner未配置")
	}
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}
	return &Runner{exec: exec, defaultTimeout: defaultTimeout}, nil
}

func (*Runner) Kind() string { return "command" }

func (r *Runner) Run(ctx context.Context, spec model.HandlerSpec, inputJSON []byte) model.Decision {
	command := strings.TrimSpace(spec.Command)
	if runtime.GOOS == "windows" && strings.TrimSpace(spec.CommandWindows) != "" {
		command = strings.TrimSpace(spec.CommandWindows)
	}
	if command == "" {
		return model.Decision{Continue: true, Err: fmt.Errorf("command Hook未配置command")}
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = r.defaultTimeout
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return model.Decision{Continue: true, Err: fmt.Errorf("Hook command 缺少 Run workspace")}
	}
	control, ok := workcontract.ControlPlaneFromContext(ctx)
	if !ok {
		return model.Decision{Continue: true, Err: fmt.Errorf("Hook command 缺少 workspace control plane")}
	}
	execution, err := control.PrepareExecution(ctx, workcontract.PrepareExecutionRequest{Subject: execmodel.ExecutionSubjectRef{TaskID: "hook:" + strings.TrimSpace(spec.Name)}, Intent: workcontract.ExecutionIntent{RequiredMode: prepared.Execution.Binding.Mode, HasProject: prepared.Execution.Binding.Mode == execmodel.WorkspaceModeProject}, RequestedAccess: execmodel.WorkspaceAccessReadWrite})
	if err != nil {
		return model.Decision{Continue: true, Err: fmt.Errorf("准备 Hook execution: %w", err)}
	}
	result, err := r.exec.Run(ctx, execmodel.Command{Command: command, Cwd: execution.Workspace.WorkDir, Stdin: inputJSON, Shell: execmodel.ShellAuto}, execcontract.RunOptions{
		Timeout: timeout, MaxOutputBytes: 128 * 1024,
		Binding: execution.Binding, Workspace: execution.Workspace,
	})
	if err != nil {
		return model.Decision{Continue: true, Err: fmt.Errorf("执行 Hook command失败: %w", err)}
	}
	decision := model.Decision{Continue: true}
	if result == nil {
		return model.Decision{Continue: true, Err: fmt.Errorf("Hook command未返回结果")}
	}
	decision.ExitCode = result.ExitCode
	if result.TimedOut {
		decision.Err = fmt.Errorf("Hook command执行超时")
		return decision
	}
	if result.ExitCode == 2 {
		decision.Continue = false
		decision.Reason = strings.TrimSpace(result.Stderr)
		return decision
	}
	if result.ExitCode != 0 {
		decision.Err = fmt.Errorf("Hook command退出码 %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr))
		return decision
	}
	if strings.TrimSpace(result.Stdout) == "" {
		return decision
	}
	if err := decodeDecision([]byte(result.Stdout), &decision); err != nil {
		decision.Err = fmt.Errorf("解析 Hook command stdout失败: %w", err)
	}
	return decision
}

func decodeDecision(raw []byte, decision *model.Decision) error {
	var output struct {
		Continue           *bool  `json:"continue"`
		SystemMessage      string `json:"systemMessage"`
		HookSpecificOutput struct {
			PermissionDecision       string         `json:"permissionDecision"`
			PermissionDecisionReason string         `json:"permissionDecisionReason"`
			UpdatedInput             map[string]any `json:"updatedInput"`
			AdditionalContext        string         `json:"additionalContext"`
			Decision                 string         `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(raw, &output); err != nil {
		return err
	}
	if output.Continue != nil {
		decision.Continue = *output.Continue
	}
	decision.SystemMessage = output.SystemMessage
	decision.PermissionDecision = normalizePermission(output.HookSpecificOutput.PermissionDecision)
	decision.Reason = output.HookSpecificOutput.PermissionDecisionReason
	decision.UpdatedInput = output.HookSpecificOutput.UpdatedInput
	decision.AdditionalContext = output.HookSpecificOutput.AdditionalContext
	if strings.EqualFold(output.HookSpecificOutput.Decision, "block") || strings.EqualFold(output.HookSpecificOutput.Decision, "deny") {
		decision.Continue = false
	}
	return nil
}

func normalizePermission(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow", "approve":
		return "allow"
	case "deny", "block":
		return "deny"
	case "ask":
		return "ask"
	default:
		return ""
	}
}
