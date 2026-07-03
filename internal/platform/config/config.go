// Package config 应用配置加载与管理
//
// 配置优先级（从低到高）：
//  1. configs/config.yaml       — 默认值，提交到版本库（不含密钥）
//  2. configs/config.local.yaml — 本地覆盖，含 API Key，已加入 .gitignore
//  3. 环境变量（前缀 AGENT_）    — CI/生产环境注入，优先级最高
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 应用全局配置。
type Config struct {
	LLM        LLMConfig        `mapstructure:"llm"`
	HTTPClient HTTPClientConfig `mapstructure:"http_client"`
	Secrets    SecretsConfig    `mapstructure:"secrets"`
	Policy     PolicyConfig     `mapstructure:"policy"`
	Agent      AgentConfig      `mapstructure:"agent"`
	Log        LogConfig        `mapstructure:"log"`
	Server     ServerConfig     `mapstructure:"server"`
}

// LLMConfig 描述 Provider 层、Model Pool 和路由规则。
type LLMConfig struct {
	Providers map[string]LLMProviderConfig `mapstructure:"providers"`
	Models    map[string]LLMModelConfig    `mapstructure:"models"`
	Router    LLMRouterConfig              `mapstructure:"router"`
	Timeout   time.Duration                `mapstructure:"timeout"`

	// 兼容旧配置：configs/config.local.yaml 中的 llm.api_key / llm.model 等。
	LegacyProvider     string `mapstructure:"provider"`
	LegacyModel        string `mapstructure:"model"`
	LegacyAPIKey       string `mapstructure:"api_key"`
	LegacyBaseURL      string `mapstructure:"base_url"`
	LegacyByAzure      bool   `mapstructure:"by_azure"`
	LegacyAPIVersion   string `mapstructure:"api_version"`
	LegacyArkAPIKey    string `mapstructure:"ark_api_key"`
	LegacyArkAccessKey string `mapstructure:"ark_access_key"`
	LegacyArkSecretKey string `mapstructure:"ark_secret_key"`
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
	Provider       string         `mapstructure:"provider"`
	Model          string         `mapstructure:"model"`
	Strategy       string         `mapstructure:"strategy"`
	Timeout        time.Duration  `mapstructure:"timeout"`
	Temperature    *float64       `mapstructure:"temperature"`
	TopP           *float64       `mapstructure:"top_p"`
	MaxTokens      int            `mapstructure:"max_tokens"`
	ToolChoice     string         `mapstructure:"tool_choice"`
	ResponseFormat string         `mapstructure:"response_format"`
	SupportsTools  *bool          `mapstructure:"supports_tools"`
	Tags           []string       `mapstructure:"tags"`
	Metadata       map[string]any `mapstructure:"metadata"`
}

// ResolvedLLMConfig 是创建模型前解析完成的配置。
type ResolvedLLMConfig struct {
	Alias          string
	ProviderName   string
	ProviderKind   string
	BaseURL        string
	APIVersion     string
	ByAzure        bool
	Headers        map[string]string
	AuthType       string
	APIKey         string
	AccessKey      string
	SecretKey      string
	Model          string
	Strategy       string
	Timeout        time.Duration
	Temperature    *float64
	TopP           *float64
	MaxTokens      int
	ToolChoice     string
	ResponseFormat string
	SupportsTools  *bool
	Tags           []string
	Metadata       map[string]any
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
	MaxIterations int    `mapstructure:"max_iterations"`
	SystemPrompt  string `mapstructure:"system_prompt"`
}

// LogConfig 日志配置。
type LogConfig struct {
	Level string `mapstructure:"level"`
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// Load 加载配置，依次读取 config.yaml → config.local.yaml → 环境变量。
func Load(configDir string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(configDir)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取 config.yaml 失败: %w", err)
	}

	local := viper.New()
	local.SetConfigName("config.local")
	local.SetConfigType("yaml")
	local.AddConfigPath(configDir)
	if err := local.ReadInConfig(); err == nil {
		for _, key := range local.AllKeys() {
			v.Set(key, local.Get(key))
		}
	}

	v.SetEnvPrefix("AGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	cfg.expandEnv()
	cfg.applyLegacyLLM()

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("配置校验失败: %w", err)
	}
	return &cfg, nil
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
	return &ResolvedLLMConfig{
		Alias:          alias,
		ProviderName:   model.Provider,
		ProviderKind:   inferProviderKind(model.Provider, provider.Type),
		BaseURL:        provider.BaseURL,
		APIVersion:     provider.APIVersion,
		ByAzure:        provider.ByAzure,
		Headers:        provider.Headers,
		AuthType:       defaultString(provider.Auth.Type, "api_key"),
		APIKey:         provider.Auth.APIKey,
		AccessKey:      provider.Auth.AccessKey,
		SecretKey:      provider.Auth.SecretKey,
		Model:          model.Model,
		Strategy:       model.Strategy,
		Timeout:        timeout,
		Temperature:    model.Temperature,
		TopP:           model.TopP,
		MaxTokens:      model.MaxTokens,
		ToolChoice:     model.ToolChoice,
		ResponseFormat: model.ResponseFormat,
		SupportsTools:  model.SupportsTools,
		Tags:           model.Tags,
		Metadata:       model.Metadata,
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
	c.LLM.LegacyAPIKey = os.ExpandEnv(c.LLM.LegacyAPIKey)
	c.LLM.LegacyBaseURL = os.ExpandEnv(c.LLM.LegacyBaseURL)
	c.LLM.LegacyAPIVersion = os.ExpandEnv(c.LLM.LegacyAPIVersion)
	c.LLM.LegacyArkAPIKey = os.ExpandEnv(c.LLM.LegacyArkAPIKey)
	c.LLM.LegacyArkAccessKey = os.ExpandEnv(c.LLM.LegacyArkAccessKey)
	c.LLM.LegacyArkSecretKey = os.ExpandEnv(c.LLM.LegacyArkSecretKey)
}

func (c *Config) applyLegacyLLM() {
	if c.LLM.LegacyProvider != "" && len(c.LLM.Providers) == 0 {
		providerName := c.LLM.LegacyProvider
		c.LLM.Providers = map[string]LLMProviderConfig{
			providerName: {
				BaseURL:    c.LLM.LegacyBaseURL,
				ByAzure:    c.LLM.LegacyByAzure,
				APIVersion: c.LLM.LegacyAPIVersion,
				Auth: LLMAuthConfig{
					Type:      "api_key",
					APIKey:    firstNonEmpty(c.LLM.LegacyAPIKey, c.LLM.LegacyArkAPIKey),
					AccessKey: c.LLM.LegacyArkAccessKey,
					SecretKey: c.LLM.LegacyArkSecretKey,
				},
			},
		}
		c.LLM.Models = map[string]LLMModelConfig{
			"default": {Provider: providerName, Model: c.LLM.LegacyModel, Strategy: "balanced"},
		}
		c.LLM.Router.Chat = "default"
	}

	chatAlias := firstNonEmpty(c.LLM.Router.Chat, c.LLM.Router.Default, c.LLM.Router.Fallback, "default")
	chatModel, ok := c.LLM.Models[chatAlias]
	if !ok {
		return
	}
	provider := c.LLM.Providers[chatModel.Provider]
	if c.LLM.LegacyAPIKey != "" && provider.Auth.APIKey == "" {
		provider.Auth.Type = defaultString(provider.Auth.Type, "api_key")
		provider.Auth.APIKey = c.LLM.LegacyAPIKey
	}
	if c.LLM.LegacyBaseURL != "" && provider.BaseURL == "" {
		provider.BaseURL = c.LLM.LegacyBaseURL
	}
	if c.LLM.LegacyByAzure {
		provider.ByAzure = true
	}
	if c.LLM.LegacyAPIVersion != "" && provider.APIVersion == "" {
		provider.APIVersion = c.LLM.LegacyAPIVersion
	}
	if c.LLM.LegacyArkAPIKey != "" && provider.Auth.APIKey == "" {
		provider.Auth.APIKey = c.LLM.LegacyArkAPIKey
	}
	if c.LLM.LegacyArkAccessKey != "" && provider.Auth.AccessKey == "" {
		provider.Auth.AccessKey = c.LLM.LegacyArkAccessKey
	}
	if c.LLM.LegacyArkSecretKey != "" && provider.Auth.SecretKey == "" {
		provider.Auth.SecretKey = c.LLM.LegacyArkSecretKey
	}
	c.LLM.Providers[chatModel.Provider] = provider
}

func validate(cfg *Config) error {
	if cfg.LLM.Timeout == 0 {
		cfg.LLM.Timeout = 60 * time.Second
	}
	applyHTTPClientDefaults(&cfg.HTTPClient)
	applySecretsDefaults(&cfg.Secrets)
	applyPolicyDefaults(&cfg.Policy)
	if err := validatePolicyConfig(cfg.Policy); err != nil {
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
