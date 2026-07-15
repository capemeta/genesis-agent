package scope

import (
	"testing"

	"genesis-agent/internal/capabilities/mcp/contract"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
)

func TestAllowsRequiresEveryConfiguredScopeDimension(t *testing.T) {
	scope := profilemodel.CapabilityScope{
		Channels:     []profilemodel.ChannelType{profilemodel.ChannelEnterprise},
		TenantIDs:    []string{"tenant-a"},
		ProjectIDs:   []string{"project-a"},
		AgentIDs:     []string{"agent-a"},
		UserIDs:      []string{"user-a"},
		RoleIDs:      []string{"operator"},
		Environments: []profilemodel.RuntimeEnvironment{profilemodel.EnvironmentServer},
	}
	env := contract.RuntimeCatalogEnv{
		Channel:     profilemodel.ChannelEnterprise,
		TenantID:    "tenant-a",
		ProjectID:   "project-a",
		AgentID:     "agent-a",
		UserID:      "user-a",
		RoleIDs:     []string{"viewer", "operator"},
		Environment: profilemodel.EnvironmentServer,
	}
	if !Allows(scope, env) {
		t.Fatal("完整匹配的 scope 应被允许")
	}

	env.UserID = ""
	if Allows(scope, env) {
		t.Fatal("受限 user scope 缺少运行时用户时必须拒绝")
	}
	env.UserID = "user-a"
	env.RoleIDs = []string{"viewer"}
	if Allows(scope, env) {
		t.Fatal("没有交集的角色 scope 必须拒绝")
	}
}
