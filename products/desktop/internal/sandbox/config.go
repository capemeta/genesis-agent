// Package sandbox 预留 Desktop 产品的可选 Docker/genesis-sandbox 模式装配点。
package sandbox

// Mode 描述 Desktop 可选择的运行环境。
type Mode string

const (
	ModeLocalHost     Mode = "local_host"
	ModePlatform      Mode = "local_platform_sandbox"
	ModeDockerSandbox Mode = "docker_sandbox"
	ModeRemoteSandbox Mode = "remote_sandbox"
)

// Config 是 Desktop sandbox profile 的最小占位配置。
type Config struct {
	Mode        Mode   `json:"mode"`
	Endpoint    string `json:"endpoint,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// TODO: genesis-sandbox 完善后，由 Desktop bootstrap 根据 Config 注入 sandbox FileSystemBackend 和 SandboxRunner。
