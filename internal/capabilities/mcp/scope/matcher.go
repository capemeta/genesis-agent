// Package scope 负责 MCP server 的能力适用范围匹配。
package scope

import (
	"strings"

	"genesis-agent/internal/capabilities/mcp/contract"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
)

// Allows 仅在每个已配置维度都匹配时允许使用能力。
// 对受限维度缺少运行时身份信息时拒绝，避免跨租户或跨主体的默认放行。
func Allows(scope profilemodel.CapabilityScope, env contract.RuntimeCatalogEnv) bool {
	return matches(scope.Channels, env.Channel) &&
		matches(scope.TenantIDs, env.TenantID) &&
		matches(scope.ProjectIDs, env.ProjectID) &&
		matches(scope.AgentIDs, env.AgentID) &&
		matches(scope.UserIDs, env.UserID) &&
		matchesAny(scope.RoleIDs, env.RoleIDs) &&
		matches(scope.Environments, env.Environment)
}

func matches[T ~string](allowed []T, actual T) bool {
	if len(allowed) == 0 {
		return true
	}
	value := strings.TrimSpace(string(actual))
	if value == "" {
		return false
	}
	for _, candidate := range allowed {
		if strings.TrimSpace(string(candidate)) == value {
			return true
		}
	}
	return false
}

func matchesAny(allowed, actual []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, value := range actual {
		if matches(allowed, value) {
			return true
		}
	}
	return false
}
