package model

// ExecutionMode 定义 Skill 的工作流沙箱执行模式。
type ExecutionMode string

const (
	// ExecutionModePerCall 针对单次/离散命令，透明路径路由模式。
	ExecutionModePerCall ExecutionMode = "per_call"
	// ExecutionModeSandboxedSession 针对生成式/多步骤 Skill，派生只有一套沙箱路径视图的子 Agent 隔离执行。
	ExecutionModeSandboxedSession ExecutionMode = "sandboxed_session"
)

// PreferredBackend 定义 Skill 偏好的沙箱后端。
type PreferredBackend string

const (
	// BackendLocalPlatform 偏好宿主机 OS 内核级进程约束沙箱 (Zero-Copy 极速直跑)。
	BackendLocalPlatform PreferredBackend = "local_platform_sandbox"
	// BackendRemoteContainer 偏好远程 Docker / genesis-sandbox 容器沙箱。
	BackendRemoteContainer PreferredBackend = "remote_sandbox"
)

// TrustLevel 定义 Skill 的安全信任等级。
type TrustLevel string

const (
	// TrustLevelVerified 自研或经人工严格审核过的可信 Skill。
	TrustLevelVerified TrustLevel = "verified"
	// TrustLevelUntrusted 社区或第三方未经审核的 Skill (默认强隔离防范)。
	TrustLevelUntrusted TrustLevel = "untrusted"
)

// SkillSandboxSpec 定义单个 Skill 内部声明或覆写的沙箱规格。
type SkillSandboxSpec struct {
	ExecutionMode    ExecutionMode    `json:"execution_mode" yaml:"execution_mode"`
	PreferredBackend PreferredBackend `json:"preferred_backend" yaml:"preferred_backend"`
	AllowDegradation bool             `json:"allow_degradation" yaml:"allow_degradation"`
	TrustLevel       TrustLevel       `json:"trust_level" yaml:"trust_level"`
	Inputs           []string         `json:"inputs" yaml:"inputs"`
	Outputs          []string         `json:"outputs" yaml:"outputs"`
}

// DefaultSkillSandboxSpec 返回缺省保守的 Skill 沙箱配置。
func DefaultSkillSandboxSpec() SkillSandboxSpec {
	return SkillSandboxSpec{
		ExecutionMode:    ExecutionModePerCall,
		PreferredBackend: BackendLocalPlatform,
		AllowDegradation: true,
		TrustLevel:       TrustLevelUntrusted,
		Inputs:           nil,
		Outputs:          nil,
	}
}

// SandboxLocalConfig 全局配置 configs/config.yaml 中的 local 节点。
type SandboxLocalConfig struct {
	Enabled      bool   `json:"enabled" yaml:"enabled"`
	Preference   string `json:"preference" yaml:"preference"`     // auto, required, disabled
	DefaultLevel string `json:"default_level" yaml:"default_level"` // process_constrained
}

// SandboxRemoteConfig 全局配置 configs/config.yaml 中的 remote 节点。
type SandboxRemoteConfig struct {
	Enabled               bool   `json:"enabled" yaml:"enabled"` // Layer 1 硬门控开关
	BaseURL               string `json:"base_url" yaml:"base_url"`
	APIKey                string `json:"api_key" yaml:"api_key"`
	APIKeyEnv             string `json:"api_key_env" yaml:"api_key_env"`
	WorkspaceID           string `json:"workspace_id" yaml:"workspace_id"`
	DefaultRuntimeProfile string `json:"default_runtime_profile" yaml:"default_runtime_profile"`
}

// SandboxRoutingConfig 全局配置 configs/config.yaml 中的 routing 节点。
type SandboxRoutingConfig struct {
	DefaultExecution     string `json:"default_execution" yaml:"default_execution"`
	AllowSessionOverride bool   `json:"allow_session_override" yaml:"allow_session_override"`
	AutoRouteRisk        bool   `json:"auto_route_risk" yaml:"auto_route_risk"`
}

// SandboxGlobalConfig 全局 configs/config.yaml 中的完整 sandbox: 配置块。
type SandboxGlobalConfig struct {
	Local          SandboxLocalConfig          `json:"local" yaml:"local"`
	Remote         SandboxRemoteConfig         `json:"remote" yaml:"remote"`
	Routing        SandboxRoutingConfig        `json:"routing" yaml:"routing"`
	SkillsOverride map[string]SkillSandboxSpec `json:"skills_override" yaml:"skills_override"`
}
