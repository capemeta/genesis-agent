// Package profile 定义 Enterprise 产品默认 Profile。
package profile

import profilemodel "genesis-agent/internal/capabilities/profile/model"

// DefaultProfile 返回 Enterprise 第一轮默认能力集。
func DefaultProfile() profilemodel.Profile {
	return profilemodel.Profile{
		ID:          "genesis-enterprise-default",
		Name:        "Genesis Enterprise Default",
		Description: "Enterprise 默认能力集；第一轮保留当前内建工具，后续接入治理策略。",
		Scope: profilemodel.CapabilityScope{
			Channels:     []profilemodel.ChannelType{profilemodel.ChannelEnterprise},
			Environments: []profilemodel.RuntimeEnvironment{profilemodel.EnvironmentServer},
		},
		Tools: profilemodel.ToolSet{
			Enabled: []string{
				"current_time",
				"calculator",
				"http_request",
			},
		},
	}
}
