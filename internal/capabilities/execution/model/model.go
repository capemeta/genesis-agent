// Package model 定义命令执行能力的数据模型。
package model

import "time"

// ShellKind 描述命令希望使用的 shell。auto 由产品侧 runner 按平台选择。
type ShellKind string

const (
	ShellAuto       ShellKind = "auto"
	ShellSystem     ShellKind = "system"
	ShellBash       ShellKind = "bash"
	ShellSh         ShellKind = "sh"
	ShellZsh        ShellKind = "zsh"
	ShellPowerShell ShellKind = "powershell"
	ShellCmd        ShellKind = "cmd"
)

// ExecutionEnvironment 描述命令实际执行环境。
type ExecutionEnvironment string

const (
	EnvironmentLocal   ExecutionEnvironment = "local"
	EnvironmentSandbox ExecutionEnvironment = "sandbox"
)

// SandboxMode 描述沙箱使用策略。
type SandboxMode string

const (
	SandboxDisabled SandboxMode = "disabled"
	SandboxOptional SandboxMode = "optional"
	SandboxRequired SandboxMode = "required"
)

// SandboxProfile 描述未来 Docker、genesis-sandbox 或平台沙箱需要的最小上下文。
type SandboxProfile struct {
	Mode        SandboxMode       `json:"mode,omitempty"`
	Provider    string            `json:"provider,omitempty"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Command 描述一次命令执行请求。
type Command struct {
	Command    string            `json:"command"`
	Cwd        string            `json:"cwd,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Shell      ShellKind         `json:"shell,omitempty"`
	Background bool              `json:"background,omitempty"`
	UsePTY     bool              `json:"use_pty,omitempty"`
}

// Result 描述命令执行结果。
type Result struct {
	Command            string               `json:"command"`
	Cwd                string               `json:"cwd"`
	Shell              ShellKind            `json:"shell,omitempty"`
	Environment        ExecutionEnvironment `json:"environment,omitempty"`
	SandboxProvider    string               `json:"sandbox_provider,omitempty"`
	Warnings           []string             `json:"warnings,omitempty"`
	// SandboxViolations 携带 OS sandbox 拒绝的结构化事件（如 macOS Seatbelt denial）。
	// 每条记录格式为 "operation:path" 或 "operation"（无路径时省略 ":"）。
	SandboxViolations  []string             `json:"sandbox_violations,omitempty"`
	ExitCode           int                  `json:"exit_code"`
	Stdout             string               `json:"stdout,omitempty"`
	Stderr             string               `json:"stderr,omitempty"`
	Duration           time.Duration        `json:"-"`
	DurationMS         int64                `json:"duration_ms"`
	TimedOut           bool                 `json:"timed_out"`
	OutputTruncated    bool                 `json:"output_truncated"`
	Error              string               `json:"error,omitempty"`
}

