package model

import (
	"time"

	profilemodel "genesis-agent/internal/capabilities/profile/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

// McpTransportType MCP 传输类型。
type McpTransportType string

const (
	McpTransportStdio          McpTransportType = "stdio"
	McpTransportStreamableHTTP McpTransportType = "streamable_http"
)

// McpPlacement 描述 MCP server 的实际放置位置。
type McpPlacement string

const (
	McpPlacementLocalStdio         McpPlacement = "local_stdio"
	McpPlacementLocalSandboxStdio  McpPlacement = "local_sandbox_stdio"
	McpPlacementRemoteSandboxStdio McpPlacement = "remote_sandbox_stdio"
	McpPlacementStreamableHTTP     McpPlacement = "streamable_http"
)

// ApprovalMode 描述 MCP server/tool 审批策略。
type ApprovalMode string

const (
	ApprovalModeAuto    ApprovalMode = "auto"
	ApprovalModePrompt  ApprovalMode = "prompt"
	ApprovalModeApprove ApprovalMode = "approve"
)

// McpServerConfig 描述单个 MCP server 的运行时配置（产品无关）。
// 安全约束：禁止 inline bearer token，只允许 BearerTokenEnv / CredentialRef 引用。
type McpServerConfig struct {
	Name     string
	Type     McpTransportType
	Enabled  bool
	Required bool

	// stdio
	Command string
	Args    []string
	Env     map[string]string
	// InheritEnv 是允许从宿主继承的环境变量名白名单。
	InheritEnv []string
	Cwd        string
	Placement  McpPlacement

	// streamable_http
	URL            string
	BearerTokenEnv string
	CredentialRef  string
	Headers        map[string]string
	EnvHeaders     map[string]string

	// 生命周期
	StartupTimeout time.Duration
	ToolTimeout    time.Duration

	// 治理
	Scope         profilemodel.CapabilityScope
	EnabledTools  []string
	DisabledTools []string
	ApprovalMode  ApprovalMode
	Exposure      tool.ToolExposure
}

// Defaults 填充缺省超时与启用状态。
func (c *McpServerConfig) Defaults(startup, toolTimeout time.Duration) {
	if c == nil {
		return
	}
	if c.Type == "" {
		c.Type = McpTransportStdio
	}
	if c.StartupTimeout <= 0 {
		if startup > 0 {
			c.StartupTimeout = startup
		} else {
			c.StartupTimeout = 30 * time.Second
		}
	}
	if c.ToolTimeout <= 0 {
		if toolTimeout > 0 {
			c.ToolTimeout = toolTimeout
		} else {
			c.ToolTimeout = 300 * time.Second
		}
	}
	if c.Exposure == "" {
		c.Exposure = tool.ToolExposureDirect
	}
}
