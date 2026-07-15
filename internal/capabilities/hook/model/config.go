package model

import "time"

// Config 是 Hook 的产品无关配置模型。
type Config struct {
	Enabled          *bool                    `yaml:"enabled" mapstructure:"enabled"`
	Execution        string                   `yaml:"execution" mapstructure:"execution"`
	AllowManagedOnly bool                     `yaml:"allow_managed_only" mapstructure:"allow_managed_only"`
	DefaultTimeout   time.Duration            `yaml:"default_timeout" mapstructure:"default_timeout"`
	Events           map[EventName][]HookSpec `yaml:"events" mapstructure:"events"`
	State            map[string]HookState     `yaml:"state" mapstructure:"state"`
}

// HookState 以稳定 handler key 覆盖单条 Hook 的启用状态和信任指纹。
type HookState struct {
	Enabled     *bool  `yaml:"enabled" mapstructure:"enabled"`
	TrustedHash string `yaml:"trusted_hash" mapstructure:"trusted_hash"`
}

// Scope 是 Hook 的统一能力适用范围配置。
type Scope struct {
	Channels     []string `mapstructure:"channels"`
	TenantIDs    []string `mapstructure:"tenant_ids"`
	ProjectIDs   []string `mapstructure:"project_ids"`
	AgentIDs     []string `mapstructure:"agent_ids"`
	UserIDs      []string `mapstructure:"user_ids"`
	RoleIDs      []string `mapstructure:"role_ids"`
	Environments []string `mapstructure:"environments"`
}

// ScopeContext 是一次运行可用的身份与运行环境事实。
type ScopeContext struct {
	Channel     string
	TenantID    string
	ProjectID   string
	AgentID     string
	UserID      string
	RoleIDs     []string
	Environment string
}

// HookSpec 是事件下的一组 matcher 与 handlers。
type HookSpec struct {
	Matcher  string        `yaml:"matcher" mapstructure:"matcher"`
	Enabled  *bool         `yaml:"enabled" mapstructure:"enabled"`
	Managed  bool          `yaml:"managed" mapstructure:"managed"`
	Scope    Scope         `yaml:"scope" mapstructure:"scope"`
	Handlers []HandlerSpec `yaml:"handlers" mapstructure:"handlers"`
}

// HandlerSpec 描述单个 Hook 处理器。
type HandlerSpec struct {
	Name           string        `yaml:"name" mapstructure:"name"`
	Type           string        `mapstructure:"type"`
	Command        string        `mapstructure:"command"`
	CommandWindows string        `mapstructure:"command_windows"`
	Builtin        string        `mapstructure:"builtin"`
	Timeout        time.Duration `mapstructure:"timeout"`
	Enabled        *bool         `mapstructure:"enabled"`
	Managed        bool          `mapstructure:"managed"`
	TrustedHash    string        `mapstructure:"trusted_hash"`
	Scope          Scope         `mapstructure:"scope"`
}

func (c Config) IsEnabled() bool      { return c.Enabled == nil || *c.Enabled }
func (s HookSpec) IsEnabled() bool    { return s.Enabled == nil || *s.Enabled }
func (s HandlerSpec) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }
