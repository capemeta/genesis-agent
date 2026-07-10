// Package profile 定义 CLI 产品默认 Profile。
package profile

import profilemodel "genesis-agent/internal/capabilities/profile/model"

// DefaultProfile 返回 CLI 第一轮默认能力集。
func DefaultProfile() profilemodel.Profile {
	return profilemodel.Profile{
		ID:          "genesis-cli-default",
		Name:        "Genesis CLI Default",
		Description: "CLI 默认能力集，保留当前内建工具并保持轻量本地运行。",
		Scope: profilemodel.CapabilityScope{
			Channels:     []profilemodel.ChannelType{profilemodel.ChannelCLI},
			Environments: []profilemodel.RuntimeEnvironment{profilemodel.EnvironmentLocal},
		},
		Tools: profilemodel.ToolSet{
			Enabled: []string{
				"current_time",
				"calculator",
				"http_request",
				"read_file",
				"write_file",
				"edit_file",
				"apply_patch",
				"list_dir",
				"walk_dir",
				"glob",
				"grep",
				"run_command",
				"run_skill_script",
				"install_skill_dependencies",
				"Skill",
				"list_skill_resources",
				"read_skill_resource",
				"search_skill_resources",
				"web_search",
				"web_fetch",
			},
		},
		Skills: profilemodel.SkillSet{AllowImplicit: true},
	}
}
