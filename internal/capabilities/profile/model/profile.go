// Package model 定义产品 Profile 的最小公共模型。
package model

// ChannelType 表示能力接入端。
type ChannelType string

const (
	ChannelCLI        ChannelType = "cli"
	ChannelDesktop    ChannelType = "desktop"
	ChannelEnterprise ChannelType = "enterprise"
)

// RuntimeEnvironment 表示运行环境。
type RuntimeEnvironment string

const (
	EnvironmentLocal   RuntimeEnvironment = "local"
	EnvironmentDesktop RuntimeEnvironment = "desktop"
	EnvironmentServer  RuntimeEnvironment = "server"
	EnvironmentSandbox RuntimeEnvironment = "sandbox"
)

// Profile 是产品默认能力配置的最小模型。
type Profile struct {
	ID          string
	Name        string
	Description string
	Scope       CapabilityScope
	Tools       ToolSet
	Skills      SkillSet
}

// CapabilityScope 描述能力适用范围。第一轮只落模型，不做租户/角色持久化。
type CapabilityScope struct {
	Channels     []ChannelType
	TenantIDs    []string
	ProjectIDs   []string
	AgentIDs     []string
	UserIDs      []string
	RoleIDs      []string
	Environments []RuntimeEnvironment
}

// ToolSet 描述工具启停策略。Disabled 优先级高于 Enabled。
type ToolSet struct {
	Enabled  []string
	Disabled []string
}

// SkillSet 描述 Skill 启停策略和来源策略。Disabled 优先级高于 Enabled。
type SkillSet struct {
	Enabled       []string
	Disabled      []string
	Sources       []SkillSourceRef
	AllowImplicit bool
}

// SkillSourceRef 是产品 profile 中声明的 Skill 来源。
type SkillSourceRef struct {
	Kind  string
	ID    string
	Scope string
	Path  string
}
