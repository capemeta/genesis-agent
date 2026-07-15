// Package config 应用配置加载与管理
//
// 配置优先级（从低到高）：
//  1. 代码内置默认值
//  2. ~/.genesis-agent/<product>/config.yaml      — 产品级用户配置
//  3. configs/config.yaml、llm.yaml、mcp.yaml      — 项目共享配置，提交到版本库（不含密钥）
//  4. configs/config.local.yaml                   — 项目本地覆盖，已加入 .gitignore
//  5. AGENT_ 环境变量                              — CI/部署/临时强制覆盖，优先级最高
//
// 普通 ${ENV_NAME} 占位符属于所在文件层：变量未定义时不覆盖低层值，显式空字符串或空列表仍会覆盖。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var (
	globalConfigSections   = []string{"agent", "context_profiles", "http_client", "log", "policy", "sandbox", "secrets", "server", "skills", "web"}
	llmConfigSections      = []string{"llm"}
	mcpConfigSections      = []string{"mcp"}
	overrideConfigSections = []string{"agent", "context_profiles", "http_client", "llm", "log", "mcp", "policy", "sandbox", "secrets", "server", "skills", "web"}
)

// Config 应用全局配置。
type Config struct {
	LLM             LLMConfig                       `mapstructure:"llm"`
	HTTPClient      HTTPClientConfig                `mapstructure:"http_client"`
	Secrets         SecretsConfig                   `mapstructure:"secrets"`
	Policy          PolicyConfig                    `mapstructure:"policy"`
	Sandbox         SandboxConfig                   `mapstructure:"sandbox"`
	Skills          SkillsConfig                    `mapstructure:"skills"`
	MCP             MCPConfig                       `mapstructure:"mcp"`
	Agent           AgentConfig                     `mapstructure:"agent"`
	Log             LogConfig                       `mapstructure:"log"`
	Server          ServerConfig                    `mapstructure:"server"`
	Web             WebConfig                       `mapstructure:"web"`
	ContextProfiles map[string]ContextProfileConfig `mapstructure:"context_profiles"` // 新增：场景化预算策略
}

// ContextProfileConfig 场景化预算配置的 YAML 映射 DTO
type ContextProfileConfig struct {
	Weights map[string]float64   `mapstructure:"weights"`
	Clamp   map[string][]float64 `mapstructure:"clamp"` // 格式 [min, max]
}

// MCPConfig 描述 MCP Client 全局配置（YAML DTO，仅 primitive 类型）。
type MCPConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// ConnectMode: background（默认，启动不阻塞）| eager（阻塞直到本轮连接结束）。
	ConnectMode           string                     `mapstructure:"connect_mode"`
	ConnectBatchSize      int                        `mapstructure:"connect_batch_size"`
	DefaultStartupTimeout time.Duration              `mapstructure:"default_startup_timeout"`
	DefaultToolTimeout    time.Duration              `mapstructure:"default_tool_timeout"`
	Servers               map[string]MCPServerConfig `mapstructure:"servers"`
}

// MCPServerConfig 是单个 MCP server 的 YAML DTO。
type MCPServerConfig struct {
	Type           string            `mapstructure:"type"`
	Enabled        *bool             `mapstructure:"enabled"`
	Required       bool              `mapstructure:"required"`
	Command        string            `mapstructure:"command"`
	Args           []string          `mapstructure:"args"`
	Env            map[string]string `mapstructure:"env"`
	Cwd            string            `mapstructure:"cwd"`
	URL            string            `mapstructure:"url"`
	BearerToken    string            `mapstructure:"bearer_token"` // 禁止使用；校验时拒绝
	BearerTokenEnv string            `mapstructure:"bearer_token_env"`
	CredentialRef  string            `mapstructure:"credential_ref"`
	Headers        map[string]string `mapstructure:"headers"`
	EnvHeaders     map[string]string `mapstructure:"env_headers"`
	StartupTimeout time.Duration     `mapstructure:"startup_timeout"`
	ToolTimeout    time.Duration     `mapstructure:"tool_timeout"`
	EnabledTools   []string          `mapstructure:"enabled_tools"`
	DisabledTools  []string          `mapstructure:"disabled_tools"`
	ApprovalMode   string            `mapstructure:"approval_mode"`
	Exposure       string            `mapstructure:"exposure"`
	Scope          MCPScopeConfig    `mapstructure:"scope"`
}

// MCPScopeConfig 是 server 级适用范围 DTO。
type MCPScopeConfig struct {
	Channels     []string `mapstructure:"channels"`
	TenantIDs    []string `mapstructure:"tenant_ids"`
	ProjectIDs   []string `mapstructure:"project_ids"`
	AgentIDs     []string `mapstructure:"agent_ids"`
	UserIDs      []string `mapstructure:"user_ids"`
	RoleIDs      []string `mapstructure:"role_ids"`
	Environments []string `mapstructure:"environments"`
}

// SandboxConfig 描述外部 sandbox API 和产品默认沙箱执行配置。
type SandboxConfig struct {
	Enabled               bool          `mapstructure:"enabled"`
	Mode                  string        `mapstructure:"mode"`
	DefaultExecution      string        `mapstructure:"default_execution"`
	AllowSessionOverride  bool          `mapstructure:"allow_session_override"`
	BaseURL               string        `mapstructure:"base_url"`
	APIKey                string        `mapstructure:"api_key"`
	APIKeyEnv             string        `mapstructure:"api_key_env"`
	WorkspaceID           string        `mapstructure:"workspace_id"`
	DefaultRuntimeProfile string        `mapstructure:"default_runtime_profile"`
	Timeout               time.Duration `mapstructure:"timeout"`
}

// WebConfig 包含网络搜索和获取工具的常用密钥与端点配置
type WebConfig struct {
	BraveAPIKey    string `mapstructure:"brave_api_key"`
	SearXNGBaseURL string `mapstructure:"searxng_base_url"`
	TavilyAPIKey   string `mapstructure:"tavily_api_key"`
	ExaAPIKey      string `mapstructure:"exa_api_key"`
	SerpAPIKey     string `mapstructure:"serpapi_api_key"`
}

// SkillsConfig 描述本地 Skill 目录和启停配置。
type SkillsConfig struct {
	Enabled               []string            `mapstructure:"enabled"`
	Disabled              []string            `mapstructure:"disabled"`
	Sources               []SkillSourceConfig `mapstructure:"sources"`
	EnablePreflight       bool                `mapstructure:"enable_preflight"`         // 默认 false
	AutoRetryAfterInstall bool                `mapstructure:"auto_retry_after_install"` // 默认 false
	Install               SkillsInstallConfig `mapstructure:"install"`
}

// SkillsInstallConfig 描述远程 Skill 安装来源策略。
type SkillsInstallConfig struct {
	// AllowedHosts 允许安装的 Git 兼容主机（支持 github.com 风格 /tree|/blob URL）。
	// 空则默认仅 github.com。
	AllowedHosts []string `mapstructure:"allowed_hosts"`
	// AllowLocal 是否允许 dir:/file: 本地安装；CLI 默认 true。
	AllowLocal *bool `mapstructure:"allow_local"`
}

// EffectiveAllowedHosts 返回安装策略主机列表（默认 github.com）。
func (c SkillsInstallConfig) EffectiveAllowedHosts() []string {
	out := make([]string, 0, len(c.AllowedHosts))
	seen := map[string]struct{}{}
	for _, h := range c.AllowedHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		h = strings.TrimPrefix(h, "https://")
		h = strings.TrimPrefix(h, "http://")
		h = strings.Trim(h, "/")
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	if len(out) == 0 {
		return []string{"github.com"}
	}
	return out
}

// EffectiveAllowLocal 本地安装开关，未配置时默认 true。
func (c SkillsInstallConfig) EffectiveAllowLocal() bool {
	if c.AllowLocal == nil {
		return true
	}
	return *c.AllowLocal
}

// SkillSourceConfig 描述一个可扫描的 Skill 来源。
type SkillSourceConfig struct {
	Kind  string `mapstructure:"kind"`
	ID    string `mapstructure:"id"`
	Scope string `mapstructure:"scope"`
	Path  string `mapstructure:"path"`
}

// LLMConfig 描述 Provider 层、Model Pool 和路由规则。
type LLMConfig struct {
	Providers map[string]LLMProviderConfig `mapstructure:"providers"`
	Models    map[string]LLMModelConfig    `mapstructure:"models"`
	Router    LLMRouterConfig              `mapstructure:"router"`
	Timeout   time.Duration                `mapstructure:"timeout"`
}

// LLMRouterConfig 描述用途到模型别名的路由。
type LLMRouterConfig struct {
	Routes        map[string]string `mapstructure:",remain"`
	Default       string            `mapstructure:"default"`
	Fallback      string            `mapstructure:"fallback"`
	ToolCall      string            `mapstructure:"tool_call"`
	Chat          string            `mapstructure:"chat"`
	Coding        string            `mapstructure:"coding"`
	Planning      string            `mapstructure:"planning"`
	Summarization string            `mapstructure:"summarization"`
	Embedding     string            `mapstructure:"embedding"`
	Vision        string            `mapstructure:"vision"`
}

// ModelAlias 返回指定用途对应的模型别名。
func (r LLMRouterConfig) ModelAlias(route string) string {
	switch route {
	case "tool_call":
		return firstNonEmpty(r.ToolCall, r.Routes[route])
	case "chat":
		return firstNonEmpty(r.Chat, r.Routes[route])
	case "coding":
		return firstNonEmpty(r.Coding, r.Routes[route])
	case "planning":
		return firstNonEmpty(r.Planning, r.Routes[route])
	case "summarization":
		return firstNonEmpty(r.Summarization, r.Routes[route])
	case "embedding":
		return firstNonEmpty(r.Embedding, r.Routes[route])
	case "vision":
		return firstNonEmpty(r.Vision, r.Routes[route])
	default:
		return r.Routes[route]
	}
}

// LLMProviderConfig Provider 层配置，只配置一次 endpoint 与认证。
type LLMProviderConfig struct {
	Type       string            `mapstructure:"type"`
	BaseURL    string            `mapstructure:"base_url"`
	APIVersion string            `mapstructure:"api_version"`
	ByAzure    bool              `mapstructure:"by_azure"`
	Timeout    time.Duration     `mapstructure:"timeout"`
	Headers    map[string]string `mapstructure:"headers"`
	Auth       LLMAuthConfig     `mapstructure:"auth"`
}

// LLMAuthConfig Provider 认证配置。
type LLMAuthConfig struct {
	Type      string `mapstructure:"type"`
	APIKey    string `mapstructure:"api_key"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
}

// LLMModelConfig Model Pool 中的轻量模型定义。
type LLMModelConfig struct {
	Provider              string         `mapstructure:"provider"`
	Model                 string         `mapstructure:"model"`
	Strategy              string         `mapstructure:"strategy"`
	Timeout               time.Duration  `mapstructure:"timeout"`
	Temperature           *float64       `mapstructure:"temperature"`
	TopP                  *float64       `mapstructure:"top_p"`
	MaxTokens             int            `mapstructure:"max_tokens"`
	ToolChoice            string         `mapstructure:"tool_choice"`
	ResponseFormat        string         `mapstructure:"response_format"`
	SupportsTools         *bool          `mapstructure:"supports_tools"`
	Tags                  []string       `mapstructure:"tags"`
	Metadata              map[string]any `mapstructure:"metadata"`
	ContextWindow         int            `mapstructure:"context_window"`          // 新增：上下文总窗口数
	EffectiveContextRatio *float64       `mapstructure:"effective_context_ratio"` // 新增：有效比例默认 0.92
	OutputReserveTokens   int            `mapstructure:"output_reserve_tokens"`   // 新增：输出预留数
}

// ResolvedLLMConfig 是创建模型前解析完成的配置。
type ResolvedLLMConfig struct {
	Alias                 string
	ProviderName          string
	ProviderKind          string
	BaseURL               string
	APIVersion            string
	ByAzure               bool
	Headers               map[string]string
	AuthType              string
	APIKey                string
	AccessKey             string
	SecretKey             string
	Model                 string
	Strategy              string
	Timeout               time.Duration
	Temperature           *float64
	TopP                  *float64
	MaxTokens             int
	ToolChoice            string
	ResponseFormat        string
	SupportsTools         *bool
	Tags                  []string
	Metadata              map[string]any
	ContextWindow         int      // 新增：解析后的窗口限制
	EffectiveContextRatio *float64 // 新增：解析后的有效比例
	OutputReserveTokens   int      // 新增：解析后的输出预留
}

// HTTPClientConfig HTTP 请求工具默认配置。
type HTTPClientConfig struct {
	DefaultTimeout        time.Duration         `mapstructure:"default_timeout"`
	SSEIdleTimeout        time.Duration         `mapstructure:"sse_idle_timeout"`
	ResponseHeaderTimeout time.Duration         `mapstructure:"response_header_timeout"`
	TLSHandshakeTimeout   time.Duration         `mapstructure:"tls_handshake_timeout"`
	IdleConnTimeout       time.Duration         `mapstructure:"idle_conn_timeout"`
	MaxIdleConns          int                   `mapstructure:"max_idle_conns"`
	MaxIdleConnsPerHost   int                   `mapstructure:"max_idle_conns_per_host"`
	MaxResponseBodyBytes  int64                 `mapstructure:"max_response_body_bytes"`
	MaxRequestBodyBytes   int64                 `mapstructure:"max_request_body_bytes"`
	MaxErrorBodyBytes     int64                 `mapstructure:"max_error_body_bytes"`
	UserAgent             string                `mapstructure:"user_agent"`
	RequestIDHeader       string                `mapstructure:"request_id_header"`
	Retry                 HTTPClientRetryConfig `mapstructure:"retry"`
}

// HTTPClientRetryConfig HTTP 请求重试配置。
type HTTPClientRetryConfig struct {
	MaxAttempts    int           `mapstructure:"max_attempts"`
	InitialBackoff time.Duration `mapstructure:"initial_backoff"`
	MaxBackoff     time.Duration `mapstructure:"max_backoff"`
	Multiplier     float64       `mapstructure:"multiplier"`
	Jitter         bool          `mapstructure:"jitter"`
}

// SecretsConfig 密钥与连接管理的本地存储配置。
type SecretsConfig struct {
	DataDir      string `mapstructure:"data_dir"`
	MasterKeyEnv string `mapstructure:"master_key_env"`
}

// PolicyConfig 描述统一权限与审批治理配置。第一批仅 files/defaults 参与运行时策略，
// commands/web/sandbox 先作为配置预留，等待对应 matcher 接入。
type PolicyConfig struct {
	Defaults PolicyDefaultsConfig `mapstructure:"defaults"`
	Files    PolicyFilesConfig    `mapstructure:"files"`
	Commands PolicyCommandsConfig `mapstructure:"commands"`
	Web      PolicyWebConfig      `mapstructure:"web"`
	Sandbox  PolicySandboxConfig  `mapstructure:"sandbox"`
}

// PolicyDefaultsConfig 描述策略默认决策。
type PolicyDefaultsConfig struct {
	Unknown            string   `mapstructure:"unknown"`
	Dangerous          string   `mapstructure:"dangerous"`
	Critical           string   `mapstructure:"critical"`
	DenyOverridesAllow bool     `mapstructure:"deny_overrides_allow"`
	AllowedGrantScopes []string `mapstructure:"allowed_grant_scopes"`
}

// PolicyFilesConfig 描述文件系统策略配置。
type PolicyFilesConfig struct {
	Default           string                  `mapstructure:"default"`
	Workspace         PolicyFileOperations    `mapstructure:"workspace"`
	External          PolicyFileOperations    `mapstructure:"external"`
	Protected         PolicyDefaultDecision   `mapstructure:"protected"`
	AllowPaths        []PolicyPathRuleConfig  `mapstructure:"allow_paths"`
	DenyPaths         []PolicyPathRuleConfig  `mapstructure:"deny_paths"`
	WorkspaceMetadata PolicyWorkspaceMetadata `mapstructure:"workspace_metadata"`
}

// PolicyFileOperations 描述文件操作到策略决策的映射。
type PolicyFileOperations struct {
	Read   string `mapstructure:"read"`
	List   string `mapstructure:"list"`
	Walk   string `mapstructure:"walk"`
	Write  string `mapstructure:"write"`
	Edit   string `mapstructure:"edit"`
	Delete string `mapstructure:"delete"`
}

// PolicyDefaultDecision 描述只包含 default 字段的策略片段。
type PolicyDefaultDecision struct {
	Default string `mapstructure:"default"`
}

// PolicyPathRuleConfig 描述路径级 allow/deny 规则。
type PolicyPathRuleConfig struct {
	Path       string   `mapstructure:"path"`
	Operations []string `mapstructure:"operations"`
	Scope      string   `mapstructure:"scope"`
}

// PolicyWorkspaceMetadata 描述工作区元数据目录策略。
type PolicyWorkspaceMetadata struct {
	Write string   `mapstructure:"write"`
	Paths []string `mapstructure:"paths"`
}

// PolicyCommandsConfig 描述命令执行策略配置。第一批仅解析 default。
type PolicyCommandsConfig struct {
	Default string `mapstructure:"default"`
}

// PolicyWebConfig 描述网络搜索与获取策略配置。第一批仅解析 default。
type PolicyWebConfig struct {
	Search PolicyDefaultDecision `mapstructure:"search"`
	Fetch  PolicyDefaultDecision `mapstructure:"fetch"`
}

// PolicySandboxConfig 描述沙箱策略配置。第一批仅解析 default_mode。
type PolicySandboxConfig struct {
	DefaultMode string `mapstructure:"default_mode"`
}

// AgentConfig Agent 运行时策略。
type AgentConfig struct {
	MaxIterations            int    `mapstructure:"max_iterations"`
	MaxConsecutiveFail       int    `mapstructure:"max_consecutive_fail"`
	RepeatGuardEnabled       *bool  `mapstructure:"repeat_guard_enabled"`
	MaxIdenticalToolFailures *int   `mapstructure:"max_identical_tool_failures"`
	MaxStagnantIterations    *int   `mapstructure:"max_stagnant_iterations"`
	SystemPrompt             string `mapstructure:"system_prompt"`
}

// LogConfig 日志配置（agent/audit/usage 三类通道）。
type LogConfig struct {
	Level    string                      `mapstructure:"level"`
	Path     string                      `mapstructure:"path"` // 兼容旧配置：指向 agent.log 时推导 dir
	Dir      string                      `mapstructure:"dir"`
	Rotate   LogRotateConfig             `mapstructure:"rotate"`
	Channels map[string]LogChannelConfig `mapstructure:"channels"`
}

// LogRotateConfig 全局滚动默认值；各 channel 可用 retain_days 覆盖保留期。
type LogRotateConfig struct {
	Daily      *bool `mapstructure:"daily"`
	MaxSizeMB  int   `mapstructure:"max_size_mb"`
	RetainDays int   `mapstructure:"retain_days"`
	Compress   bool  `mapstructure:"compress"`
}

// DailyEnabled 未配置时默认按日滚动。
func (r LogRotateConfig) DailyEnabled() bool {
	if r.Daily == nil {
		return true
	}
	return *r.Daily
}

// LogChannelConfig 单通道配置。
type LogChannelConfig struct {
	Enabled    *bool  `mapstructure:"enabled"`
	File       string `mapstructure:"file"`
	Format     string `mapstructure:"format"` // agent: text|json；audit/usage: jsonl
	RetainDays int    `mapstructure:"retain_days"`
	Level      string `mapstructure:"level"`
}

// ChannelEnabled 未配置时默认启用。
func (c LogChannelConfig) ChannelEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

const defaultConfigTemplate = `# Genesis Agent CLI 用户配置
# 由 Genesis CLI 自动生成。仅取消注释需要覆盖仓库默认值的配置。

# web:
#   brave_api_key: ""

# sandbox:
#   mode: local_host

# llm:
#   providers:
#     qwen:
#       auth:
#         api_key: "" # 也可设置 AGENT_LLM_PROVIDERS_QWEN_AUTH_API_KEY。

skills:
  # Extra local skill roots scanned by CLI.
  # Project default .genesis/skills and user default ~/.genesis-agent/cli/skills
  # are added automatically; configure additional roots here when needed.
  sources: []
  install:
    allowed_hosts:
      - github.com
`

// Load 加载 CLI 配置，保留旧调用点兼容。
func Load(configDir string) (*Config, error) {
	return LoadForProduct(configDir, "cli")
}

// LoadForProduct 加载指定产品配置。
func LoadForProduct(configDir string, product string) (*Config, error) {
	return LoadWithOptions(configDir, LoadOptions{Product: product})
}

// LoadOptions 描述配置加载选项。ConfigHome 主要用于测试或产品自定义用户目录。
type LoadOptions struct {
	Product          string
	ConfigHome       string
	EnsureUserConfig bool
}

// LoadWithOptions 按用户级、项目共享、项目本地和 AGENT_ 环境变量的优先级加载配置。
func LoadWithOptions(configDir string, opts LoadOptions) (*Config, error) {
	product := strings.TrimSpace(opts.Product)
	if product == "" {
		product = "cli"
	}
	settings := map[string]any{}
	projectSettings := map[string]any{}

	hasBaseConfig, err := mergeYAMLConfigFile(projectSettings, filepath.Join(configDir, "config.yaml"), false, globalConfigSections)
	if err != nil {
		return nil, err
	}
	if _, err := mergeYAMLConfigFile(projectSettings, filepath.Join(configDir, "llm.yaml"), hasBaseConfig, llmConfigSections); err != nil {
		return nil, err
	}
	if _, err := mergeYAMLConfigFile(projectSettings, filepath.Join(configDir, "mcp.yaml"), false, mcpConfigSections); err != nil {
		return nil, err
	}

	userPath, hasUserPath := userConfigPath(opts, product)
	if hasUserPath {
		if opts.EnsureUserConfig || !hasBaseConfig {
			if err := ensureProductUserConfig(userPath); err != nil {
				return nil, err
			}
		}
		if _, err := mergeYAMLConfigFile(settings, userPath, false, overrideConfigSections); err != nil {
			return nil, err
		}
	}

	mergeConfigMap(settings, projectSettings)
	if _, err := mergeYAMLConfigFile(settings, filepath.Join(configDir, "config.local.yaml"), false, overrideConfigSections); err != nil {
		return nil, err
	}

	if !hasBaseConfig && hasUserPath {
		if _, err := os.Stat(userPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("读取 config.yaml 失败: base config not found and user config not generated")
		}
	}

	v := viper.New()
	if err := v.MergeConfigMap(cloneConfigMap(settings)); err != nil {
		return nil, fmt.Errorf("初始化合并配置失败: %w", err)
	}
	v.SetEnvPrefix("AGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	restoreMCPMapKeys(&cfg, settings)

	cfg.applyExplicitEnvOverrides()
	cfg.decryptDPAPISecrets()
	cfg.expandEnv()

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("配置校验失败: %w", err)
	}
	return &cfg, nil
}

// mergeYAMLConfigFile 将单个 YAML 文件按调用顺序合并到目标配置。
// allowedTopLevel 约束文件职责边界；local/用户配置可覆盖所有正式配置域。
func mergeYAMLConfigFile(target map[string]any, path string, required bool, allowedTopLevel []string) (bool, error) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if required {
				return false, fmt.Errorf("缺少必需配置文件 %s", path)
			}
			return false, nil
		}
		return false, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	}
	settings := map[string]any{}
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return false, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
	}

	if len(allowedTopLevel) > 0 {
		allowed := make(map[string]struct{}, len(allowedTopLevel))
		for _, key := range allowedTopLevel {
			allowed[key] = struct{}{}
		}
		for key := range settings {
			if _, ok := allowed[key]; !ok {
				return false, fmt.Errorf("配置文件 %s 不允许顶层配置 %q", path, key)
			}
		}
	}
	mergeConfigMap(target, settings)
	return true, nil
}

// restoreMCPMapKeys 恢复 Viper 为大小写不敏感查找而折叠的 MCP server 与环境变量键。
func restoreMCPMapKeys(cfg *Config, settings map[string]any) {
	mcpSettings, ok := settings["mcp"].(map[string]any)
	if !ok {
		return
	}
	serverSettings, ok := mcpSettings["servers"].(map[string]any)
	if !ok {
		return
	}
	restored := make(map[string]MCPServerConfig, len(serverSettings))
	for name, raw := range serverSettings {
		server, ok := cfg.MCP.Servers[name]
		if !ok {
			server = cfg.MCP.Servers[strings.ToLower(name)]
		}
		rawServer, _ := raw.(map[string]any)
		server.Env = stringMap(rawServer["env"], server.Env)
		server.Headers = stringMap(rawServer["headers"], server.Headers)
		server.EnvHeaders = stringMap(rawServer["env_headers"], server.EnvHeaders)
		restored[name] = server
	}
	cfg.MCP.Servers = restored
}

func stringMap(raw any, fallback map[string]string) map[string]string {
	values, ok := raw.(map[string]any)
	if !ok {
		return fallback
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		if text, ok := value.(string); ok {
			out[key] = text
			continue
		}
		out[key] = fmt.Sprint(value)
	}
	return out
}

// mergeConfigMap 采用“map 递归合并、标量与列表整体覆盖”的确定性规则合并高优先级配置。
func mergeConfigMap(target, override map[string]any) {
	for key, value := range override {
		if unresolvedEnvPlaceholder(value) {
			continue
		}
		overrideMap, overrideIsMap := value.(map[string]any)
		currentMap, currentIsMap := target[key].(map[string]any)
		if overrideIsMap && currentIsMap {
			mergeConfigMap(currentMap, overrideMap)
			continue
		}
		target[key] = value
	}
}

func unresolvedEnvPlaceholder(value any) bool {
	text, ok := value.(string)
	if !ok || len(text) < 4 || !strings.HasPrefix(text, "${") || !strings.HasSuffix(text, "}") {
		return false
	}
	name := text[2 : len(text)-1]
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "${}") {
		return false
	}
	_, exists := os.LookupEnv(name)
	return !exists
}

func cloneConfigMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		switch typed := value.(type) {
		case map[string]any:
			cloned[key] = cloneConfigMap(typed)
		case []any:
			items := make([]any, len(typed))
			copy(items, typed)
			cloned[key] = items
		default:
			cloned[key] = value
		}
	}
	return cloned
}

func ensureProductUserConfig(configPath string) error {
	configPath = filepath.Clean(configPath)
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("创建用户配置目录失败: %w", err)
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(defaultConfigTemplate), 0600); err != nil {
			return fmt.Errorf("创建用户默认配置文件失败: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("检查用户配置文件失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "skills"), 0755); err != nil {
		return fmt.Errorf("创建用户 skills 目录失败: %w", err)
	}
	return nil
}
func userConfigPath(opts LoadOptions, product string) (string, bool) {
	root := strings.TrimSpace(opts.ConfigHome)
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", false
		}
		root = filepath.Join(home, ".genesis-agent")
	}
	return filepath.Join(root, product, "config.yaml"), true
}
func (c *Config) applyExplicitEnvOverrides() {
	for name, provider := range c.LLM.Providers {
		prefix := "AGENT_LLM_PROVIDERS_" + envNamePart(name)
		if value := strings.TrimSpace(os.Getenv(prefix + "_TYPE")); value != "" {
			provider.Type = value
		}
		if value := strings.TrimSpace(os.Getenv(prefix + "_BASE_URL")); value != "" {
			provider.BaseURL = value
		}
		if value := strings.TrimSpace(os.Getenv(prefix + "_API_VERSION")); value != "" {
			provider.APIVersion = value
		}
		if value := strings.TrimSpace(os.Getenv(prefix + "_AUTH_TYPE")); value != "" {
			provider.Auth.Type = value
		}
		if value := strings.TrimSpace(os.Getenv(prefix + "_AUTH_API_KEY")); value != "" {
			provider.Auth.APIKey = value
		}
		if value := strings.TrimSpace(os.Getenv(prefix + "_AUTH_ACCESS_KEY")); value != "" {
			provider.Auth.AccessKey = value
		}
		if value := strings.TrimSpace(os.Getenv(prefix + "_AUTH_SECRET_KEY")); value != "" {
			provider.Auth.SecretKey = value
		}
		c.LLM.Providers[name] = provider
	}
	if value := strings.TrimSpace(os.Getenv("AGENT_WEB_BRAVE_API_KEY")); value != "" {
		c.Web.BraveAPIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENT_WEB_SEARXNG_BASE_URL")); value != "" {
		c.Web.SearXNGBaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENT_WEB_TAVILY_API_KEY")); value != "" {
		c.Web.TavilyAPIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENT_WEB_EXA_API_KEY")); value != "" {
		c.Web.ExaAPIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENT_WEB_SERPAPI_API_KEY")); value != "" {
		c.Web.SerpAPIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENT_SANDBOX_ENABLED")); value != "" {
		c.Sandbox.Enabled = strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "yes")
	}
	if value := strings.TrimSpace(os.Getenv("AGENT_SANDBOX_MODE")); value != "" {
		c.Sandbox.Mode = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENT_SANDBOX_DEFAULT_EXECUTION")); value != "" {
		c.Sandbox.DefaultExecution = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENT_SANDBOX_BASE_URL")); value != "" {
		c.Sandbox.BaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENT_SANDBOX_API_KEY")); value != "" {
		c.Sandbox.APIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENT_SANDBOX_WORKSPACE_ID")); value != "" {
		c.Sandbox.WorkspaceID = value
	}
}

func envNamePart(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(value)
	return value
}
func (c *Config) decryptDPAPISecrets() {
	decryptField := func(field *string) {
		if strings.HasPrefix(*field, "dpapi:") {
			ciphertext := strings.TrimPrefix(*field, "dpapi:")
			if plaintext, err := Decrypt(ciphertext); err == nil {
				*field = plaintext
			}
		}
	}

	// Decrypt LLM key fields
	for name, prov := range c.LLM.Providers {
		auth := prov.Auth
		decryptField(&auth.APIKey)
		decryptField(&auth.AccessKey)
		decryptField(&auth.SecretKey)
		prov.Auth = auth
		c.LLM.Providers[name] = prov
	}

	// Decrypt Web key fields
	decryptField(&c.Web.BraveAPIKey)
	decryptField(&c.Web.TavilyAPIKey)
	decryptField(&c.Web.ExaAPIKey)
	decryptField(&c.Web.SerpAPIKey)
	decryptField(&c.Sandbox.APIKey)

}

// ResolveRoute 根据用途路由解析模型；未知用途回退 chat/default。
func (c LLMConfig) ResolveRoute(route string) (*ResolvedLLMConfig, error) {
	alias := c.Router.ModelAlias(route)
	if alias == "" {
		alias = firstNonEmpty(c.Router.Chat, c.Router.Default, c.Router.Fallback, c.Router.ModelAlias("chat"), "default")
	}
	return c.ResolveModel(alias)
}

// ResolveModel 解析 Model Pool 中的模型别名。
func (c LLMConfig) ResolveModel(alias string) (*ResolvedLLMConfig, error) {
	model, ok := c.Models[alias]
	if !ok {
		return nil, fmt.Errorf("llm.models.%s 未定义", alias)
	}
	provider, ok := c.Providers[model.Provider]
	if !ok {
		return nil, fmt.Errorf("llm.models.%s.provider=%q 未在 providers 中定义", alias, model.Provider)
	}
	timeout := model.Timeout
	if timeout == 0 {
		timeout = provider.Timeout
	}
	if timeout == 0 {
		timeout = c.Timeout
	}
	contextWindow := model.ContextWindow
	if contextWindow <= 0 {
		if isTestEnv() {
			contextWindow = 128000
		} else {
			return nil, fmt.Errorf("配置文件中的 llm.models.%s 缺少必填字段 'context_window'（或该值非正整数）。为了保证上下文预算裁剪和防止溢出机制的安全运行，必须在配置中显式指定该模型所支持的最大上下文窗口（例如：context_window: 128000）", alias)
		}
	}

	return &ResolvedLLMConfig{
		Alias:                 alias,
		ProviderName:          model.Provider,
		ProviderKind:          inferProviderKind(model.Provider, provider.Type),
		BaseURL:               provider.BaseURL,
		APIVersion:            provider.APIVersion,
		ByAzure:               provider.ByAzure,
		Headers:               provider.Headers,
		AuthType:              defaultString(provider.Auth.Type, "api_key"),
		APIKey:                provider.Auth.APIKey,
		AccessKey:             provider.Auth.AccessKey,
		SecretKey:             provider.Auth.SecretKey,
		Model:                 model.Model,
		Strategy:              model.Strategy,
		Timeout:               timeout,
		Temperature:           model.Temperature,
		TopP:                  model.TopP,
		MaxTokens:             model.MaxTokens,
		ToolChoice:            model.ToolChoice,
		ResponseFormat:        model.ResponseFormat,
		SupportsTools:         model.SupportsTools,
		Tags:                  model.Tags,
		Metadata:              model.Metadata,
		ContextWindow:         contextWindow,
		EffectiveContextRatio: model.EffectiveContextRatio,
		OutputReserveTokens:   model.OutputReserveTokens,
	}, nil
}

func (c *Config) expandEnv() {
	for name, provider := range c.LLM.Providers {
		provider.BaseURL = os.ExpandEnv(provider.BaseURL)
		provider.APIVersion = os.ExpandEnv(provider.APIVersion)
		provider.Auth.APIKey = os.ExpandEnv(provider.Auth.APIKey)
		provider.Auth.AccessKey = os.ExpandEnv(provider.Auth.AccessKey)
		provider.Auth.SecretKey = os.ExpandEnv(provider.Auth.SecretKey)
		for k, v := range provider.Headers {
			provider.Headers[k] = os.ExpandEnv(v)
		}
		c.LLM.Providers[name] = provider
	}
	c.Web.BraveAPIKey = os.ExpandEnv(c.Web.BraveAPIKey)
	c.Web.SearXNGBaseURL = os.ExpandEnv(c.Web.SearXNGBaseURL)
	c.Web.TavilyAPIKey = os.ExpandEnv(c.Web.TavilyAPIKey)
	c.Web.ExaAPIKey = os.ExpandEnv(c.Web.ExaAPIKey)
	c.Web.SerpAPIKey = os.ExpandEnv(c.Web.SerpAPIKey)
	c.Sandbox.BaseURL = os.ExpandEnv(c.Sandbox.BaseURL)
	c.Sandbox.APIKey = os.ExpandEnv(c.Sandbox.APIKey)
	c.Sandbox.WorkspaceID = os.ExpandEnv(c.Sandbox.WorkspaceID)
	if strings.TrimSpace(c.Sandbox.APIKey) == "" && strings.TrimSpace(c.Sandbox.APIKeyEnv) != "" {
		c.Sandbox.APIKey = strings.TrimSpace(os.Getenv(strings.TrimSpace(c.Sandbox.APIKeyEnv)))
	}
	for i := range c.Skills.Sources {
		c.Skills.Sources[i].Path = os.ExpandEnv(c.Skills.Sources[i].Path)
	}
	for name, server := range c.MCP.Servers {
		server.Command = os.ExpandEnv(server.Command)
		server.Cwd = os.ExpandEnv(server.Cwd)
		server.URL = os.ExpandEnv(server.URL)
		for i, arg := range server.Args {
			server.Args[i] = os.ExpandEnv(arg)
		}
		for key, value := range server.Env {
			server.Env[key] = os.ExpandEnv(value)
		}
		for key, value := range server.Headers {
			server.Headers[key] = os.ExpandEnv(value)
		}
		c.MCP.Servers[name] = server
	}
}

func validate(cfg *Config) error {
	if cfg.LLM.Timeout == 0 {
		cfg.LLM.Timeout = 60 * time.Second
	}
	applyHTTPClientDefaults(&cfg.HTTPClient)
	applySecretsDefaults(&cfg.Secrets)
	applySandboxDefaults(&cfg.Sandbox)
	applyPolicyDefaults(&cfg.Policy)
	applyLogDefaults(&cfg.Log)
	applyMCPDefaults(&cfg.MCP)
	if err := validatePolicyConfig(cfg.Policy); err != nil {
		return err
	}
	if err := validateSandboxConfig(cfg.Sandbox); err != nil {
		return err
	}
	if err := validateLogConfig(cfg.Log); err != nil {
		return err
	}
	if err := validateMCPConfig(cfg.MCP); err != nil {
		return err
	}
	if len(cfg.LLM.Providers) == 0 {
		return fmt.Errorf("llm.providers 不能为空")
	}
	if len(cfg.LLM.Models) == 0 {
		return fmt.Errorf("llm.models 不能为空")
	}
	if _, err := cfg.LLM.ResolveRoute("chat"); err != nil {
		return err
	}
	for alias := range cfg.LLM.Models {
		resolved, err := cfg.LLM.ResolveModel(alias)
		if err != nil {
			return err
		}
		if resolved.Model == "" {
			return fmt.Errorf("llm.models.%s.model 不能为空", alias)
		}
	}
	return nil
}

func applyHTTPClientDefaults(cfg *HTTPClientConfig) {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}
	if cfg.ResponseHeaderTimeout <= 0 {
		cfg.ResponseHeaderTimeout = 15 * time.Second
	}
	if cfg.TLSHandshakeTimeout <= 0 {
		cfg.TLSHandshakeTimeout = 10 * time.Second
	}
	if cfg.IdleConnTimeout <= 0 {
		cfg.IdleConnTimeout = 90 * time.Second
	}
	if cfg.MaxIdleConns <= 0 {
		cfg.MaxIdleConns = 100
	}
	if cfg.MaxIdleConnsPerHost <= 0 {
		cfg.MaxIdleConnsPerHost = 10
	}
	if cfg.MaxResponseBodyBytes <= 0 {
		cfg.MaxResponseBodyBytes = 4 << 20
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		cfg.MaxRequestBodyBytes = 4 << 20
	}
	if cfg.MaxErrorBodyBytes <= 0 {
		cfg.MaxErrorBodyBytes = 4 << 10
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "genesis-agent/httpclient"
	}
	if cfg.RequestIDHeader == "" {
		cfg.RequestIDHeader = "X-Request-ID"
	}
	if cfg.Retry.MaxAttempts <= 0 {
		cfg.Retry.MaxAttempts = 3
	}
	if cfg.Retry.InitialBackoff <= 0 {
		cfg.Retry.InitialBackoff = 200 * time.Millisecond
	}
	if cfg.Retry.MaxBackoff <= 0 {
		cfg.Retry.MaxBackoff = 2 * time.Second
	}
	if cfg.Retry.Multiplier <= 0 {
		cfg.Retry.Multiplier = 2
	}
}

func applySecretsDefaults(cfg *SecretsConfig) {
	if strings.TrimSpace(cfg.DataDir) == "" {
		cfg.DataDir = "data"
	}
	if strings.TrimSpace(cfg.MasterKeyEnv) == "" {
		cfg.MasterKeyEnv = "GENESIS_MASTER_KEY"
	}
}

func applySandboxDefaults(cfg *SandboxConfig) {
	if strings.TrimSpace(cfg.Mode) == "" {
		cfg.Mode = "local_host"
	}
	if strings.TrimSpace(cfg.DefaultExecution) == "" {
		cfg.DefaultExecution = "disabled"
	}
	if strings.TrimSpace(cfg.APIKeyEnv) == "" {
		cfg.APIKeyEnv = "GENESIS_SANDBOX_API_KEY"
	}
	if strings.TrimSpace(cfg.DefaultRuntimeProfile) == "" {
		cfg.DefaultRuntimeProfile = "code-polyglot-basic"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
}

func applyPolicyDefaults(cfg *PolicyConfig) {
	if strings.TrimSpace(cfg.Defaults.Unknown) == "" {
		cfg.Defaults.Unknown = "ask"
	}
	if strings.TrimSpace(cfg.Defaults.Dangerous) == "" {
		cfg.Defaults.Dangerous = "ask"
	}
	if strings.TrimSpace(cfg.Defaults.Critical) == "" {
		cfg.Defaults.Critical = "deny"
	}
	cfg.Defaults.DenyOverridesAllow = true
	if len(cfg.Defaults.AllowedGrantScopes) == 0 {
		cfg.Defaults.AllowedGrantScopes = []string{"once", "turn", "session", "project"}
	}
	if strings.TrimSpace(cfg.Files.Default) == "" {
		cfg.Files.Default = "ask"
	}
	defaultFileOps(&cfg.Files.Workspace, "allow", "allow", "allow", "allow", "allow", "ask")
	defaultFileOps(&cfg.Files.External, "ask", "ask", "ask", "ask", "ask", "deny")
	if strings.TrimSpace(cfg.Files.Protected.Default) == "" {
		cfg.Files.Protected.Default = "deny"
	}
	if strings.TrimSpace(cfg.Files.WorkspaceMetadata.Write) == "" {
		cfg.Files.WorkspaceMetadata.Write = "deny"
	}
	if len(cfg.Files.WorkspaceMetadata.Paths) == 0 {
		cfg.Files.WorkspaceMetadata.Paths = []string{".git", ".agents", ".codex"}
	}
	if strings.TrimSpace(cfg.Commands.Default) == "" {
		cfg.Commands.Default = "ask"
	}
	if strings.TrimSpace(cfg.Web.Search.Default) == "" {
		cfg.Web.Search.Default = "ask"
	}
	if strings.TrimSpace(cfg.Web.Fetch.Default) == "" {
		cfg.Web.Fetch.Default = "ask"
	}
	if strings.TrimSpace(cfg.Sandbox.DefaultMode) == "" {
		cfg.Sandbox.DefaultMode = "disabled"
	}
}

func applyLogDefaults(cfg *LogConfig) {
	if strings.TrimSpace(cfg.Level) == "" {
		cfg.Level = "info"
	}
	// 旧 path 兼容：指向 .../agent.log 时推导 dir 与 agent 文件名。
	if strings.TrimSpace(cfg.Dir) == "" {
		path := strings.TrimSpace(cfg.Path)
		if path != "" {
			cfg.Dir = filepath.ToSlash(filepath.Dir(path))
			base := filepath.Base(path)
			if strings.HasSuffix(strings.ToLower(base), ".log") {
				if cfg.Channels == nil {
					cfg.Channels = map[string]LogChannelConfig{}
				}
				ch := cfg.Channels["agent"]
				if strings.TrimSpace(ch.File) == "" {
					ch.File = base
					cfg.Channels["agent"] = ch
				}
			}
		}
	}
	if strings.TrimSpace(cfg.Dir) == "" {
		cfg.Dir = ".genesis/logs"
	}
	if cfg.Rotate.MaxSizeMB <= 0 {
		cfg.Rotate.MaxSizeMB = 100
	}
	if cfg.Rotate.RetainDays <= 0 {
		cfg.Rotate.RetainDays = 14
	}
	if cfg.Channels == nil {
		cfg.Channels = map[string]LogChannelConfig{}
	}
	cfg.Channels["agent"] = mergeLogChannel(cfg.Channels["agent"], LogChannelConfig{
		File: "agent.log", Format: "text", RetainDays: 14, Level: cfg.Level,
	})
	cfg.Channels["audit"] = mergeLogChannel(cfg.Channels["audit"], LogChannelConfig{
		File: "audit.log", Format: "jsonl", RetainDays: 90, Level: "info",
	})
	cfg.Channels["usage"] = mergeLogChannel(cfg.Channels["usage"], LogChannelConfig{
		File: "usage.log", Format: "jsonl", RetainDays: 90, Level: "info",
	})
}

func mergeLogChannel(cur, def LogChannelConfig) LogChannelConfig {
	if cur.Enabled == nil {
		cur.Enabled = def.Enabled
		if cur.Enabled == nil {
			enabled := true
			cur.Enabled = &enabled
		}
	}
	if strings.TrimSpace(cur.File) == "" {
		cur.File = def.File
	}
	if strings.TrimSpace(cur.Format) == "" {
		cur.Format = def.Format
	}
	if cur.RetainDays <= 0 {
		cur.RetainDays = def.RetainDays
	}
	if strings.TrimSpace(cur.Level) == "" {
		cur.Level = def.Level
	}
	return cur
}

func validateLogConfig(cfg LogConfig) error {
	if strings.TrimSpace(cfg.Dir) == "" {
		return fmt.Errorf("log.dir 不能为空")
	}
	if cfg.Rotate.MaxSizeMB <= 0 {
		return fmt.Errorf("log.rotate.max_size_mb 必须大于 0")
	}
	for name, ch := range cfg.Channels {
		if !ch.ChannelEnabled() {
			continue
		}
		if strings.TrimSpace(ch.File) == "" {
			return fmt.Errorf("log.channels.%s.file 不能为空", name)
		}
		format := strings.ToLower(strings.TrimSpace(ch.Format))
		switch name {
		case "agent":
			if format != "text" && format != "json" {
				return fmt.Errorf("log.channels.agent.format 必须是 text 或 json")
			}
		case "audit", "usage":
			if format != "jsonl" {
				return fmt.Errorf("log.channels.%s.format 必须是 jsonl", name)
			}
		}
		if ch.RetainDays <= 0 {
			return fmt.Errorf("log.channels.%s.retain_days 必须大于 0", name)
		}
	}
	return nil
}

func defaultFileOps(cfg *PolicyFileOperations, read, list, walk, write, edit, delete string) {
	if strings.TrimSpace(cfg.Read) == "" {
		cfg.Read = read
	}
	if strings.TrimSpace(cfg.List) == "" {
		cfg.List = list
	}
	if strings.TrimSpace(cfg.Walk) == "" {
		cfg.Walk = walk
	}
	if strings.TrimSpace(cfg.Write) == "" {
		cfg.Write = write
	}
	if strings.TrimSpace(cfg.Edit) == "" {
		cfg.Edit = edit
	}
	if strings.TrimSpace(cfg.Delete) == "" {
		cfg.Delete = delete
	}
}

func applyMCPDefaults(cfg *MCPConfig) {
	if cfg == nil {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.ConnectMode))
	if mode == "" {
		cfg.ConnectMode = "background"
	} else {
		cfg.ConnectMode = mode
	}
	if cfg.ConnectBatchSize <= 0 {
		cfg.ConnectBatchSize = 3
	}
	if cfg.DefaultStartupTimeout <= 0 {
		cfg.DefaultStartupTimeout = 30 * time.Second
	}
	if cfg.DefaultToolTimeout <= 0 {
		cfg.DefaultToolTimeout = 300 * time.Second
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]MCPServerConfig{}
	}
}

func validateMCPConfig(cfg MCPConfig) error {
	if !cfg.Enabled {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(cfg.ConnectMode)) {
	case "", "background", "eager":
	default:
		return fmt.Errorf("mcp.connect_mode 仅支持 background 或 eager")
	}
	if cfg.ConnectBatchSize > 50 {
		return fmt.Errorf("mcp.connect_batch_size 不能超过 50")
	}
	for name, server := range cfg.Servers {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("mcp.servers 存在空名称")
		}
		typ := strings.ToLower(strings.TrimSpace(server.Type))
		if typ == "" {
			typ = "stdio"
		}
		switch typ {
		case "stdio":
			if strings.TrimSpace(server.Command) == "" {
				return fmt.Errorf("mcp.servers.%s: stdio 需要 command", name)
			}
		case "streamable_http":
			if strings.TrimSpace(server.URL) == "" {
				return fmt.Errorf("mcp.servers.%s: streamable_http 需要 url", name)
			}
		default:
			return fmt.Errorf("mcp.servers.%s: type 必须是 stdio 或 streamable_http", name)
		}
		if strings.TrimSpace(server.BearerToken) != "" {
			return fmt.Errorf("mcp.servers.%s: 禁止 inline bearer_token，请使用 bearer_token_env 或 credential_ref", name)
		}
		for hk, hv := range server.Headers {
			key := strings.ToLower(strings.TrimSpace(hk))
			val := strings.TrimSpace(hv)
			if key == "authorization" || strings.HasPrefix(strings.ToLower(val), "bearer ") {
				return fmt.Errorf("mcp.servers.%s: headers 禁止放置 Authorization/Bearer 明文，请使用 bearer_token_env 或 env_headers", name)
			}
		}
		if exp := strings.TrimSpace(server.Exposure); exp != "" {
			switch exp {
			case "direct", "deferred", "hidden":
			default:
				return fmt.Errorf("mcp.servers.%s: exposure 必须是 direct、deferred 或 hidden", name)
			}
		}
	}
	return nil
}

func validateSandboxConfig(cfg SandboxConfig) error {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "local_host", "local_platform_sandbox", "docker_sandbox", "remote_sandbox":
	default:
		return fmt.Errorf("sandbox.mode 必须是 local_host、local_platform_sandbox、docker_sandbox 或 remote_sandbox")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.DefaultExecution)) {
	case "disabled", "optional", "required":
	default:
		return fmt.Errorf("sandbox.default_execution 必须是 disabled、optional 或 required")
	}
	if cfg.Enabled {
		switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
		case "docker_sandbox", "remote_sandbox":
			if strings.TrimSpace(cfg.BaseURL) == "" {
				return fmt.Errorf("sandbox.enabled=true 且 mode=%s 时 sandbox.base_url 不能为空", cfg.Mode)
			}
		}
	}
	return nil
}

func validatePolicyConfig(cfg PolicyConfig) error {
	decisions := map[string]bool{"allow": true, "ask": true, "deny": true}
	checks := map[string]string{
		"policy.defaults.unknown":               cfg.Defaults.Unknown,
		"policy.defaults.dangerous":             cfg.Defaults.Dangerous,
		"policy.defaults.critical":              cfg.Defaults.Critical,
		"policy.files.default":                  cfg.Files.Default,
		"policy.files.workspace.read":           cfg.Files.Workspace.Read,
		"policy.files.workspace.list":           cfg.Files.Workspace.List,
		"policy.files.workspace.walk":           cfg.Files.Workspace.Walk,
		"policy.files.workspace.write":          cfg.Files.Workspace.Write,
		"policy.files.workspace.edit":           cfg.Files.Workspace.Edit,
		"policy.files.workspace.delete":         cfg.Files.Workspace.Delete,
		"policy.files.external.read":            cfg.Files.External.Read,
		"policy.files.external.list":            cfg.Files.External.List,
		"policy.files.external.walk":            cfg.Files.External.Walk,
		"policy.files.external.write":           cfg.Files.External.Write,
		"policy.files.external.edit":            cfg.Files.External.Edit,
		"policy.files.external.delete":          cfg.Files.External.Delete,
		"policy.files.protected.default":        cfg.Files.Protected.Default,
		"policy.files.workspace_metadata.write": cfg.Files.WorkspaceMetadata.Write,
		"policy.commands.default":               cfg.Commands.Default,
		"policy.web.search.default":             cfg.Web.Search.Default,
		"policy.web.fetch.default":              cfg.Web.Fetch.Default,
	}
	for name, value := range checks {
		if !decisions[strings.ToLower(strings.TrimSpace(value))] {
			return fmt.Errorf("%s 必须是 allow、ask 或 deny", name)
		}
	}
	for _, scope := range cfg.Defaults.AllowedGrantScopes {
		switch strings.ToLower(strings.TrimSpace(scope)) {
		case "once", "turn", "session", "project":
		case "tenant", "global":
			return fmt.Errorf("policy.defaults.allowed_grant_scopes 不允许普通交互 scope %q", scope)
		default:
			return fmt.Errorf("policy.defaults.allowed_grant_scopes 包含未知 scope %q", scope)
		}
	}
	for i, rule := range append(append([]PolicyPathRuleConfig{}, cfg.Files.AllowPaths...), cfg.Files.DenyPaths...) {
		if strings.TrimSpace(rule.Path) == "" {
			return fmt.Errorf("policy.files path rule[%d].path 不能为空", i)
		}
		for _, op := range rule.Operations {
			if !validPolicyFileOperation(op) {
				return fmt.Errorf("policy.files path rule[%d].operations 包含未知操作 %q", i, op)
			}
		}
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Sandbox.DefaultMode)) {
	case "disabled", "optional", "required":
	default:
		return fmt.Errorf("policy.sandbox.default_mode 必须是 disabled、optional 或 required")
	}
	return nil
}

func validPolicyFileOperation(op string) bool {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "read", "list", "walk", "write", "edit", "delete":
		return true
	default:
		return false
	}
}

func inferProviderKind(name string, configuredType string) string {
	if configuredType != "" {
		return strings.ToLower(configuredType)
	}
	switch strings.ToLower(name) {
	case "ollama":
		return "ollama"
	case "ark", "doubao", "volcengine":
		return "ark"
	case "claude", "anthropic":
		return "anthropic"
	default:
		return "openai"
	}
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func isTestEnv() bool {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test.") || strings.Contains(arg, "test.v") {
			return true
		}
	}
	return false
}
