// Package profile 定义 CLI 产品默认 Profile。
package profile

import profilemodel "genesis-agent/internal/capabilities/profile/model"

// DefaultProfile 返回 CLI 第一轮默认能力集，并按实际配置声明 MCP 工具。
func DefaultProfile(mcpEnabled bool) profilemodel.Profile {
	prof := profilemodel.Profile{
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
				"todo_read",
				"todo_write",
				"todo_update_step",
				"enter_plan_mode",
				"exit_plan_mode",
				"write_implementation_plan",
				"read_file",
				"view_image",
				"write_file",
				"edit_file",
				"apply_patch",
				"list_dir",
				"walk_dir",
				"glob",
				"grep",
				"run_command",
				"write_stdin",
				"run_skill_command",
				"select_deliverable_candidate",
				"install_skill_dependencies",
				"install_skill_from_source",
				"Skill",
				"Task",
				"TaskOutput",
				"TaskStop",
				"list_skill_resources",
				"read_skill_resource",
				"search_skill_resources",
				"web_search",
				"web_fetch",
			},
		},
		Skills: profilemodel.SkillSet{AllowImplicit: true},
		TurnInput: profilemodel.TurnInputSettings{
			DocumentExtract: "path_only",
			MentionResolve:  "off",
		},
	}
	if mcpEnabled {
		prof.Tools.Enabled = append(prof.Tools.Enabled, "list_mcp_resources", "read_mcp_resource", "search_mcp_tools", "mcp__*")
	}
	return prof
}
