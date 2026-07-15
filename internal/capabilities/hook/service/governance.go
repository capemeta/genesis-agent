package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"genesis-agent/internal/capabilities/hook/model"
)

// HandlerKey 返回与配置位置无关的稳定 handler 身份。
func HandlerKey(event model.EventName, matcher string, spec model.HandlerSpec) string {
	canonical := strings.Join([]string{string(event), strings.TrimSpace(matcher), strings.ToLower(strings.TrimSpace(spec.Type)), strings.TrimSpace(spec.Builtin), strings.TrimSpace(spec.Command), strings.TrimSpace(spec.CommandWindows)}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return string(event) + ":sha256:" + hex.EncodeToString(sum[:])
}

// HandlerFingerprint 返回 command handler 的信任内容指纹。
func HandlerFingerprint(spec model.HandlerSpec) string {
	canonical := strings.Join([]string{strings.ToLower(strings.TrimSpace(spec.Type)), strings.TrimSpace(spec.Command), strings.TrimSpace(spec.CommandWindows)}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func scopeMatches(rule model.Scope, actual model.ScopeContext) bool {
	return matchesScopeValues(rule.Channels, actual.Channel) &&
		matchesScopeValues(rule.TenantIDs, actual.TenantID) &&
		matchesScopeValues(rule.ProjectIDs, actual.ProjectID) &&
		matchesScopeValues(rule.AgentIDs, actual.AgentID) &&
		matchesScopeValues(rule.UserIDs, actual.UserID) &&
		matchesScopeValues(rule.Environments, actual.Environment) &&
		matchesAnyScopeValue(rule.RoleIDs, actual.RoleIDs)
}

func matchesScopeValues(allowed []string, actual string) bool {
	if len(allowed) == 0 {
		return true
	}
	actual = strings.TrimSpace(actual)
	for _, value := range allowed {
		value = strings.TrimSpace(value)
		if value == "*" || (value != "" && value == actual) {
			return true
		}
	}
	return false
}

func matchesAnyScopeValue(allowed, actual []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range actual {
		if matchesScopeValues(allowed, candidate) {
			return true
		}
	}
	return false
}
