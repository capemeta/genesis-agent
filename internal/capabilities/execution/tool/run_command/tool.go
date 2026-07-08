// Package run_command 实现通用命令执行工具。
package run_command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/capabilities/execution/policy"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
	"genesis-agent/internal/platform/contextutil"
)

const (
	defaultTimeout        = 30 * time.Second
	maxTimeout            = 10 * time.Minute
	defaultMaxOutputBytes = int64(128 * 1024)
	maxOutputBytes        = int64(4 * 1024 * 1024)
)

// Deps 是 run_command 工具依赖。
type Deps struct {
	Runner         execcontract.ExecutionRunner
	SessionManager execcontract.InteractiveSessionRunner // 注入会话管理器端口（可选，向后兼容）
	Resolver       fscontract.PathResolver
	Approval       approvalcontract.Service
	Locker         scheduler.ResourceLocker
	Sandbox        execmodel.SandboxProfile
	BridgeTerminal func(ctx context.Context, sessionID string) error // 桥接终端交互回调 (可选)
}

// Validate 校验依赖。
func (d Deps) Validate() error {
	if d.Runner == nil {
		return fmt.Errorf("ExecutionRunner未配置")
	}
	if d.Resolver == nil {
		return fmt.Errorf("PathResolver未配置")
	}
	if d.Approval == nil {
		return fmt.Errorf("ApprovalService未配置")
	}
	if d.Locker == nil {
		return fmt.Errorf("ResourceLocker未配置")
	}
	return nil
}

// Tool 执行平台 shell 命令。
type Tool struct {
	deps Deps
}

type input struct {
	Command        string            `json:"command"`
	Cwd            string            `json:"cwd,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Shell          string            `json:"shell,omitempty"`
	TimeoutMS      int64             `json:"timeout_ms,omitempty"`
	MaxOutputBytes int64             `json:"max_output_bytes,omitempty"`
	Background     bool              `json:"background,omitempty"`
	UsePTY         bool              `json:"use_pty,omitempty"`
}

// New 创建 run_command 工具。
func New(deps Deps) (tool.Tool, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "run_command",
		Description: "在当前 workspace 内或经审批目录下执行平台 shell 命令。支持按产品注入本地或沙箱 runner，并限制超时和输出大小。支持后台异步运行与 PTY 交互会话。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"command":          {Type: "string", Description: "要执行的命令"},
				"cwd":              {Type: "string", Description: "工作目录，默认当前 workspace"},
				"env":              {Type: "object", Description: "额外环境变量；配置后需要审批"},
				"shell":            {Type: "string", Description: "shell类型：auto/system/bash/sh/zsh/powershell/cmd，默认auto；具体支持取决于产品runner"},
				"timeout_ms":       {Type: "integer", Description: "超时时间，默认30000，最大600000"},
				"max_output_bytes": {Type: "integer", Description: "stdout和stderr分别输出上限，默认131072，最大4194304"},
				"background":       {Type: "boolean", Description: "是否在后台异步运行，默认false"},
				"use_pty":          {Type: "boolean", Description: "是否启用 PTY 交互会话，默认false"},
			},
			Required: []string{"command"},
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := decodeParams(params, &in); err != nil {
		return "", err
	}
	in.Command = strings.TrimSpace(in.Command)
	if in.Command == "" {
		return "", execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("command不能为空"))
	}
	shell, err := parseShell(in.Shell)
	if err != nil {
		return "", err
	}
	cwdRaw := strings.TrimSpace(in.Cwd)
	if cwdRaw == "" {
		cwdRaw = "."
	}
	cwd, err := t.deps.Resolver.Resolve(ctx, fsmodel.PathRef{Raw: cwdRaw}, fscontract.ResolveOptions{
		Operation:        "command.exec",
		MustExist:        true,
		AllowDirectory:   true,
		RequireDirectory: true,
	})
	if err != nil {
		return "", err
	}
	cmd := execmodel.Command{
		Command:    in.Command,
		Cwd:        cwd.BackendPath,
		Env:        in.Env,
		Shell:      shell,
		Background: in.Background,
		UsePTY:     in.UsePTY,
	}
	cls := policy.Classify(cmd.Command)
	if len(cmd.Env) > 0 && !cls.Critical {
		cls.Dangerous = true
		if cls.Reason == "read-only command" {
			cls.Reason = "custom environment requires approval"
		}
	}
	decision, err := t.deps.Approval.Authorize(ctx, policy.BuildApprovalRequest("run_command", cmd, cwd, cls))
	if err != nil {
		return "", err
	}
	if !isApproved(decision) {
		return "", execcontract.NewError(execcontract.ErrCodePermissionDenied, fmt.Errorf("approval %s: %s", decision.Type, decision.Reason))
	}

	mode := scheduler.LockWrite
	if cls.ReadOnly && !cls.Dangerous && !cls.Destructive {
		mode = scheduler.LockRead
	}
	release, err := t.deps.Locker.Acquire(ctx, []scheduler.ResourceLock{{Scope: "workspace", Key: workspaceKey(cwd), Mode: mode}})
	if err != nil {
		return "", err
	}
	defer release()

	runOpts := execcontract.RunOptions{
		Timeout:        timeoutOf(in.TimeoutMS),
		MaxOutputBytes: outputLimitOf(in.MaxOutputBytes),
		Sandbox:        sandboxProfile(ctx, t.deps.Sandbox),
	}
	if runOpts.Sandbox.Metadata == nil {
		runOpts.Sandbox.Metadata = make(map[string]string)
	}
	runOpts.Sandbox.Metadata["path_scope"] = string(cwd.Scope)

	// 如果指定了后台运行或伪终端，则交由 SessionManager 管理
	if in.Background || in.UsePTY {
		if t.deps.SessionManager == nil {
			return "", execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("SessionManager未配置，不支持PTY或后台执行"))
		}

		sessionID, ok := contextutil.GetSessionID(ctx)
		if !ok || strings.TrimSpace(sessionID) == "" {
			sessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
		}

		err = t.deps.SessionManager.StartSession(ctx, sessionID, cmd, runOpts)
		if err != nil {
			return "", err
		}

		// 异步后台运行模式：直接返回会话 ID 句柄，不阻塞 Agent
		if in.Background {
			res := map[string]any{
				"session_id": sessionID,
				"status":     "running",
				"message":    "命令已在后台异步启动，可使用 write_stdin 工具与会话订阅工具交互与查看进度",
			}
			data, _ := json.MarshalIndent(res, "", "  ")
			return string(data), nil
		}

		// 如果是同步 PTY 模式且配置了终端物理桥接回调，则直接调用物理接管桥接
		if t.deps.BridgeTerminal != nil {
			err = t.deps.BridgeTerminal(ctx, sessionID)
			if err != nil {
				_ = t.deps.SessionManager.KillSession(context.Background(), sessionID)
				return "", err
			}
			res := &execmodel.Result{
				Command:     cmd.Command,
				Cwd:         cmd.Cwd,
				Shell:       cmd.Shell,
				ExitCode:    0,
				Environment: execmodel.EnvironmentLocal,
			}
			return toJSON(res)
		}

		// 同步 PTY 交互模式：订阅日志并同步等待会话运行结束
		outputCh, subCancel, err := t.deps.SessionManager.SubscribeOutput(ctx, sessionID)
		if err != nil {
			return "", err
		}
		defer subCancel()

		var buf strings.Builder
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

		timeout := timeoutOf(in.TimeoutMS)
		deadline := time.Now().Add(timeout)

		for {
			select {
			case <-ctx.Done():
				_ = t.deps.SessionManager.KillSession(context.Background(), sessionID)
				return "", ctx.Err()
			case data, ok := <-outputCh:
				if ok {
					buf.Write(data)
				}
			case <-ticker.C:
				status, sOk, err := t.deps.SessionManager.GetSessionStatus(ctx, sessionID)
				if err != nil || !sOk || status != execmodel.SessionStatusRunning {
					// 进程退出，清空剩余通道缓存
					for {
						select {
						case data, ok := <-outputCh:
							if ok {
								buf.Write(data)
								continue
							}
						default:
						}
						break
					}
					// 终止并返回
					res := &execmodel.Result{
						Command:     cmd.Command,
						Cwd:         cmd.Cwd,
						Shell:       cmd.Shell,
						Stdout:      buf.String(),
						ExitCode:    0,
						Environment: execmodel.EnvironmentLocal,
					}
					return toJSON(res)
				}

				if time.Now().After(deadline) {
					_ = t.deps.SessionManager.KillSession(context.Background(), sessionID)
					return "", execcontract.NewError(execcontract.ErrCodeTimeout, fmt.Errorf("PTY session execution timed out"))
				}
			}
		}
	}

	// 模式 A：默认单次同步管道执行
	result, err := t.deps.Runner.Run(ctx, cmd, runOpts)
	if err != nil {
		return "", err
	}
	return toJSON(result)
}

func sandboxProfile(ctx context.Context, profile execmodel.SandboxProfile) execmodel.SandboxProfile {
	if override, ok := contextutil.GetSandboxProfileOverride(ctx); ok {
		switch v := override.(type) {
		case execmodel.SandboxProfile:
			profile = v
		case *execmodel.SandboxProfile:
			if v != nil {
				profile = *v
			}
		}
	}
	if profile.Mode == "" {
		profile.Mode = execmodel.SandboxDisabled
	}
	return profile
}

func parseShell(raw string) (execmodel.ShellKind, error) {
	shell := execmodel.ShellKind(strings.TrimSpace(strings.ToLower(raw)))
	if shell == "" {
		return execmodel.ShellAuto, nil
	}
	switch shell {
	case execmodel.ShellAuto, execmodel.ShellSystem, execmodel.ShellBash, execmodel.ShellSh, execmodel.ShellZsh, execmodel.ShellPowerShell, execmodel.ShellCmd:
		return shell, nil
	default:
		return "", execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("未知shell类型: %s", raw))
	}
}

func isApproved(decision approvalmodel.Decision) bool {
	return decision.Type == approvalmodel.DecisionApproved || decision.Type == approvalmodel.DecisionApprovedForScope
}

func workspaceKey(path fsmodel.ResolvedPath) string {
	if path.WorkspaceID != "" {
		return path.WorkspaceID
	}
	return "default"
}

func timeoutOf(ms int64) time.Duration {
	if ms <= 0 {
		return defaultTimeout
	}
	d := time.Duration(ms) * time.Millisecond
	if d > maxTimeout {
		return maxTimeout
	}
	return d
}

func outputLimitOf(limit int64) int64 {
	if limit <= 0 {
		return defaultMaxOutputBytes
	}
	if limit > maxOutputBytes {
		return maxOutputBytes
	}
	return limit
}

func decodeParams(params string, dst any) error {
	decoder := json.NewDecoder(strings.NewReader(params))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("参数解析失败: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("参数只能包含一个JSON对象")
	}
	return nil
}

func toJSON(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化工具结果失败: %w", err)
	}
	return string(data), nil
}
