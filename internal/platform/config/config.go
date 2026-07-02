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
