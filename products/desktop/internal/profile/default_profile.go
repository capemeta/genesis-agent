package profile

import profilemodel "genesis-agent/internal/capabilities/profile/model"

// DefaultProfile 返回 Desktop 当前已装配能力集，并按实际配置声明 MCP 工具。
func DefaultProfile(mcpEnabled bool) profilemodel.Profile {
	prof := profilemodel.Profile{
		ID:          "genesis-desktop-default",
		Name:        "Genesis Desktop Default",
		Description: "Desktop 当前能力集；Artifact 控制面已装配，Skill 工具栈尚未启用（启用 run_skill_command 时须像 CLI 注入 Reservations/Finalizer）。",
		Scope: profilemodel.CapabilityScope{
			Channels:     []profilemodel.ChannelType{profilemodel.ChannelDesktop},
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
				"view_image",
				"Task",
				"TaskOutput",
				"TaskStop",
			},
		},
		Skills: profilemodel.SkillSet{AllowImplicit: false},
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
