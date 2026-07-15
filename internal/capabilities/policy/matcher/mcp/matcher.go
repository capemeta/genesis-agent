package mcp

import (
	"context"
	"strings"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/platform/config"
)

// Matcher 处理 ActionMCPCall / mcp:// 资源与 mcp__* 工具名通配。
type Matcher struct {
	defaults config.PolicyDefaultsConfig
	// Rules 可选：精确或前缀规则；空则按 defaults.Unknown / Dangerous 回退。
	AllowPrefixes []string
	DenyPrefixes  []string
	DefaultAsk    bool
}

// New 创建 MCP policy matcher。
func New(defaults config.PolicyDefaultsConfig) *Matcher {
	return &Matcher{defaults: defaults, DefaultAsk: true}
}

// Match 命中 mcp.call 或资源 URI 以 mcp:// 开头时返回策略结果。
func (m *Matcher) Match(ctx context.Context, req approvalmodel.Request) (approvalmodel.PolicyResult, bool, error) {
	if err := ctx.Err(); err != nil {
		return approvalmodel.PolicyResult{}, false, err
	}
	if req.Action != approvalmodel.ActionMCPCall && !strings.HasPrefix(req.Resource.URI, "mcp://") && !strings.HasPrefix(req.ToolName, "mcp__") {
		return approvalmodel.PolicyResult{}, false, nil
	}

	resource := firstNonEmpty(req.Resource.URI, req.ToolName)
	for _, p := range m.DenyPrefixes {
		if matchPrefix(resource, p) || matchPrefix(req.ToolName, p) {
			return approvalmodel.PolicyResult{
				Type:            approvalmodel.PolicyDeny,
				Reason:          "mcp denied by policy prefix: " + p,
				Risk:            approvalmodel.RiskHigh,
				SuggestedScopes: req.SuggestedScopes,
			}, true, nil
		}
	}
	for _, p := range m.AllowPrefixes {
		if matchPrefix(resource, p) || matchPrefix(req.ToolName, p) {
			return approvalmodel.PolicyResult{
				Type:            approvalmodel.PolicyAllow,
				Reason:          "mcp allowed by policy prefix: " + p,
				Risk:            approvalmodel.RiskMedium,
				SuggestedScopes: req.SuggestedScopes,
			}, true, nil
		}
	}

	decision := strings.ToLower(strings.TrimSpace(m.defaults.Dangerous))
	if decision == "" {
		decision = strings.ToLower(strings.TrimSpace(m.defaults.Unknown))
	}
	if m.DefaultAsk && decision == "" {
		decision = "ask"
	}
	return resultOf(decision, "mcp default policy", approvalmodel.RiskMedium, req.SuggestedScopes), true, nil
}

func matchPrefix(value, pattern string) bool {
	value = strings.TrimSpace(value)
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || value == "" {
		return false
	}
	if pattern == "*" || pattern == value {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

func resultOf(decision, reason string, risk approvalmodel.RiskLevel, scopes []approvalmodel.GrantScope) approvalmodel.PolicyResult {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "allow":
		return approvalmodel.PolicyResult{Type: approvalmodel.PolicyAllow, Reason: reason, Risk: risk, SuggestedScopes: scopes}
	case "deny":
		return approvalmodel.PolicyResult{Type: approvalmodel.PolicyDeny, Reason: reason, Risk: risk, SuggestedScopes: scopes}
	default:
		return approvalmodel.PolicyResult{Type: approvalmodel.PolicyAsk, Reason: reason, Risk: risk, SuggestedScopes: scopes}
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
