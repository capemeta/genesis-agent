// Package profile 定义 Enterprise 产品默认 Profile。
package profile

import profilemodel "genesis-agent/internal/capabilities/profile/model"

// DefaultProfile 返回 Enterprise 第一轮默认能力集。
func DefaultProfile() profilemodel.Profile {
	return profilemodel.Profile{
		ID:          "genesis-enterprise-default",
		Name:        "Genesis Enterprise Default",
		Description: "Enterprise 默认能力集；内置 Skills + run_skill_command 已接线，远程沙箱/RBAC 后续接入。",
		Scope: profilemodel.CapabilityScope{
			Channels:     []profilemodel.ChannelType{profilemodel.ChannelEnterprise},
			Environments: []profilemodel.RuntimeEnvironment{profilemodel.EnvironmentServer},
		},
		Tools: profilemodel.ToolSet{
			Enabled: []string{
				"current_time",
				"calculator",
				"http_request",
				"run_skill_command",
				"install_skill_dependencies",
				"Skill",
				"list_skill_resources",
				"read_skill_resource",
				"search_skill_resources",
			},
		},
		Skills: profilemodel.SkillSet{AllowImplicit: true},
	}
}

