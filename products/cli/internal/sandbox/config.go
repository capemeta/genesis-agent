// Package sandbox 预留 CLI 产品的可选 Docker/genesis-sandbox 模式装配点。
package sandbox

import (
	"fmt"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// Mode 描述 CLI 可选择的运行环境。
type Mode string

const (
	ModeLocalHost     Mode = "local_host"
	ModePlatform      Mode = "local_platform_sandbox"
	ModeDockerSandbox Mode = "docker_sandbox"
	ModeRemoteSandbox Mode = "remote_sandbox"
)

const ProviderLocalPlatform = "local-platform"

// Config 是 CLI sandbox profile 的最小占位配置。
type Config struct {
	Mode        Mode                  `json:"mode"`
	Execution   execmodel.SandboxMode `json:"execution"`
	Endpoint    string                `json:"endpoint,omitempty"`
	WorkspaceID string                `json:"workspace_id,omitempty"`
}

// DefaultConfig 返回 CLI 默认运行配置。默认不启用沙箱，保持现有行为。
func DefaultConfig() Config {
	return Config{Mode: ModeLocalHost, Execution: execmodel.SandboxDisabled}
}

// ParseFlag 解析 CLI 暴露的 --sandbox 参数。
func ParseFlag(raw string) (Config, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(execmodel.SandboxDisabled), "off", "false", "none":
		return DefaultConfig(), nil
	case string(execmodel.SandboxOptional), "auto":
		return Config{Mode: ModePlatform, Execution: execmodel.SandboxOptional}, nil
	case string(execmodel.SandboxRequired), "require":
		return Config{Mode: ModePlatform, Execution: execmodel.SandboxRequired}, nil
	default:
		return Config{}, fmt.Errorf("未知sandbox模式 %q，可选值: disabled, optional, required", raw)
	}
}

// ExecutionProfile 转成 execution 能力域使用的 SandboxProfile。
func (c Config) ExecutionProfile() execmodel.SandboxProfile {
	mode := c.Execution
	if mode == "" {
		mode = execmodel.SandboxDisabled
	}
	profile := execmodel.SandboxProfile{Mode: mode}
	if mode != execmodel.SandboxDisabled {
		profile.Provider = ProviderLocalPlatform
		profile.Metadata = map[string]string{
			"filesystem": "workspace_write",
			"network":    "disabled",
		}
	}
	return profile
}

// TODO: genesis-sandbox 完善后，由 CLI bootstrap 根据 Config 注入 sandbox FileSystemBackend 和 SandboxRunner。
