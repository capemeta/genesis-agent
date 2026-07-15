package profile

import profilemodel "genesis-agent/internal/capabilities/profile/model"

// DefaultProfile 返回 Desktop 首期默认能力集（与 CLI 对齐工具白名单，Scope 为 desktop/local）。
func DefaultProfile() profilemodel.Profile {
	return profilemodel.Profile{
		ID:          "genesis-desktop-default",
		Name:        "Genesis Desktop Default",
		Description: "Desktop 默认能力集；MCP/工具内核与 CLI 共享，UI 由 Wails 承载。",
		Scope: profilemodel.CapabilityScope{
			Channels:     []profilemodel.ChannelType{profilemodel.ChannelDesktop},
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
				"Skill",
				"Task",
				"TaskOutput",
				"TaskStop",
				"list_mcp_resources",
				"read_mcp_resource",
				"mcp_search",
				"mcp__*",
			},
		},
		Skills: profilemodel.SkillSet{AllowImplicit: true},
	}
}
