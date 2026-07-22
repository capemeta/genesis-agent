// Package sandbox 预留 CLI 产品的可选 Docker/genesis-sandbox 模式装配点。
package sandbox

import (
	"fmt"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	platformconfig "genesis-agent/internal/platform/config"
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
const ProviderGenesisSandbox = "genesis-sandbox"

// Config 是 CLI sandbox profile 的最小占位配置。
type Config struct {
	Mode                  Mode                            `json:"mode"`
	Execution             execmodel.SandboxMode           `json:"execution"`
	Endpoint              string                          `json:"endpoint,omitempty"`
	APIKey                string                          `json:"api_key,omitempty"`
	WorkspaceID           string                          `json:"workspace_id,omitempty"`
	DefaultRuntimeProfile execmodel.SandboxRuntimeProfile `json:"default_runtime_profile,omitempty"`
	AllowSessionOverride  bool                            `json:"allow_session_override,omitempty"`
}

// DefaultConfig 返回 CLI 默认运行配置。默认不启用沙箱，保持现有行为。
func DefaultConfig() Config {
	return Config{Mode: ModeLocalHost, Execution: execmodel.SandboxDisabled}
}

// FromRuntimeConfig 将全局 sandbox 配置转成 CLI 产品运行配置。
func FromRuntimeConfig(cfg platformconfig.SandboxConfig) (Config, error) {
	if !cfg.Enabled {
		return DefaultConfig(), nil
	}
	mode := Mode(strings.ToLower(strings.TrimSpace(cfg.Mode)))
	if mode == "" {
		mode = ModeLocalHost
	}
	if !cfg.Remote.Enabled && (mode == ModeDockerSandbox || mode == ModeRemoteSandbox) {
		if cfg.Local.Enabled {
			mode = ModePlatform
		} else {
			mode = ModeLocalHost
		}
	}
	execution := execmodel.SandboxMode(strings.ToLower(strings.TrimSpace(cfg.DefaultExecution)))
	if execution == "" {
		execution = execmodel.SandboxDisabled
	}
	result := Config{
		Mode:                  mode,
		Execution:             execution,
		Endpoint:              strings.TrimSpace(cfg.BaseURL),
		APIKey:                strings.TrimSpace(cfg.APIKey),
		WorkspaceID:           strings.TrimSpace(cfg.WorkspaceID),
		DefaultRuntimeProfile: execmodel.SandboxRuntimeProfile(strings.TrimSpace(cfg.DefaultRuntimeProfile)),
		AllowSessionOverride:  cfg.AllowSessionOverride,
	}
	switch mode {
	case ModeLocalHost, ModePlatform, ModeDockerSandbox, ModeRemoteSandbox:
	default:
		return Config{}, fmt.Errorf("未知sandbox运行模式 %q", cfg.Mode)
	}
	switch execution {
	case execmodel.SandboxDisabled, execmodel.SandboxOptional, execmodel.SandboxRequired:
	default:
		return Config{}, fmt.Errorf("未知sandbox执行策略 %q", cfg.DefaultExecution)
	}
	return result, nil
}

// ParseFlag 解析 CLI 暴露的 --sandbox 参数。
func ParseFlag(raw string) (Config, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(execmodel.SandboxDisabled), "off", "false", "none":
		return DefaultConfig(), nil
	case string(execmodel.SandboxOptional), "auto":
		return Config{Execution: execmodel.SandboxOptional}, nil
	case string(execmodel.SandboxRequired), "require":
		return Config{Execution: execmodel.SandboxRequired}, nil
	default:
		return Config{}, fmt.Errorf("未知sandbox模式 %q，可选值: disabled, optional, required", raw)
	}
}

// MergeSessionOverride 将会话级开关合并到全局 sandbox 配置。
func MergeSessionOverride(base Config, override Config) Config {
	if override.Mode != "" {
		base.Mode = override.Mode
	}
	if override.Execution != "" {
		base.Execution = override.Execution
		if base.Execution != execmodel.SandboxDisabled && base.Mode == ModeLocalHost {
			base.Mode = ModePlatform
		}
	}
	if override.Endpoint != "" {
		base.Endpoint = override.Endpoint
	}
	if override.APIKey != "" {
		base.APIKey = override.APIKey
	}
	if override.WorkspaceID != "" {
		base.WorkspaceID = override.WorkspaceID
	}
	if override.DefaultRuntimeProfile != "" {
		base.DefaultRuntimeProfile = override.DefaultRuntimeProfile
	}
	if override.AllowSessionOverride {
		base.AllowSessionOverride = true
	}
	return base
}

// ExecutionProfile 转成 execution 能力域使用的 SandboxProfile。
func (c Config) ExecutionProfile() execmodel.SandboxProfile {
	mode := c.Execution
	if mode == "" {
		mode = execmodel.SandboxDisabled
	}
	profile := execmodel.SandboxProfile{Mode: mode}
	if mode != execmodel.SandboxDisabled {
		profile.Provider = providerForMode(c.Mode)
		profile.WorkspaceID = c.WorkspaceID
		profile.RuntimeProfile = c.DefaultRuntimeProfile
		if profile.RuntimeProfile == "" {
			profile.RuntimeProfile = execmodel.RuntimeProfileCodePolyglotBasic
		}
		profile.TaskType = execmodel.SandboxTaskShell
		profile.Operation = execmodel.SandboxOperationRunShell
		profile.Language = "shell"
		profile.RiskLevel = execmodel.SandboxRiskMedium
		profile.Metadata = map[string]string{
			"filesystem": "workspace_write",
			"network":    "disabled",
		}
	}
	return profile
}

func providerForMode(mode Mode) string {
	switch mode {
	case ModeDockerSandbox, ModeRemoteSandbox:
		return ProviderGenesisSandbox
	case ModePlatform:
		return ProviderLocalPlatform
	default:
		return ""
	}
}

// TODO: genesis-sandbox 文件 API 稳定后，由 CLI bootstrap 根据 Config 注入 sandbox FileSystemBackend。
