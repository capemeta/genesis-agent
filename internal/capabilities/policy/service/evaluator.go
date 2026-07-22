// Package service 提供统一策略评估服务。
package service

import (
	"context"
	"strings"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	fspermission "genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/policy/contract"
	"genesis-agent/internal/platform/config"
)

// Evaluator 按 matcher 顺序执行策略评估，并保证 deny 优先。
// 当配置或 Context 提供 PermissionMode 时：matcher Deny 仍最高优先；
// Mode 的 Allow/Deny 覆盖 matcher Ask；Mode Ask 时再回落到 matcher / metadata。
type Evaluator struct {
	defaults       config.PolicyDefaultsConfig
	permissionMode fspermission.PermissionMode
	matchers       []contract.Matcher
}

// NewEvaluator 创建策略评估器。
func NewEvaluator(defaults config.PolicyDefaultsConfig, matchers ...contract.Matcher) *Evaluator {
	return &Evaluator{defaults: defaults, matchers: matchers}
}

// WithPermissionMode 设置默认 PermissionMode（可被 Context 覆盖）。
func (e *Evaluator) WithPermissionMode(mode string) *Evaluator {
	if e == nil {
		return nil
	}
	raw := strings.TrimSpace(mode)
	if raw == "" {
		e.permissionMode = ""
		return e
	}
	e.permissionMode = fspermission.NormalizeMode(raw)
	return e
}

// Evaluate 评估一次请求。
func (e *Evaluator) Evaluate(ctx context.Context, req approvalmodel.Request) (approvalmodel.PolicyResult, error) {
	if err := ctx.Err(); err != nil {
		return approvalmodel.PolicyResult{}, err
	}
	var firstNonDeny *approvalmodel.PolicyResult
	for _, matcher := range e.matchers {
		if matcher == nil {
			continue
		}
		result, ok, err := matcher.Match(ctx, req)
		if err != nil {
			return approvalmodel.PolicyResult{}, err
		}
		if !ok {
			continue
		}
		result = e.normalize(result, req)
		if result.Type == approvalmodel.PolicyDeny {
			return result, nil
		}
		if firstNonDeny == nil {
			copy := result
			firstNonDeny = &copy
		}
	}

	if mode := e.resolveMode(ctx, req); mode != "" {
		modeRes, err := fspermission.NewModeEvaluator().Evaluate(ctx, mode, req)
		if err != nil {
			return approvalmodel.PolicyResult{}, err
		}
		modeRes = e.normalize(modeRes, req)
		switch modeRes.Type {
		case approvalmodel.PolicyDeny, approvalmodel.PolicyAllow:
			return modeRes, nil
		case approvalmodel.PolicyAsk:
			if firstNonDeny != nil {
				return *firstNonDeny, nil
			}
			return modeRes, nil
		}
	}

	if firstNonDeny != nil {
		return *firstNonDeny, nil
	}
	if result, ok := e.metadataFallback(req); ok {
		return e.normalize(result, req), nil
	}
	return e.normalize(approvalmodel.PolicyResult{Type: decisionOf(e.defaults.Unknown), Reason: "policy default for unknown operation", Risk: riskOrDefault(req.Risk)}, req), nil
}

func (e *Evaluator) resolveMode(ctx context.Context, req approvalmodel.Request) fspermission.PermissionMode {
	if ctxMode, ok := fspermission.FromContext(ctx); ok && ctxMode != "" {
		return fspermission.NormalizeMode(string(ctxMode))
	}
	metadata := mergeMetadata(req)
	if rawMode := firstNonEmptyString(metadata["permission_mode"], metadata["mode"]); rawMode != "" {
		return fspermission.NormalizeMode(rawMode)
	}
	if e.permissionMode != "" {
		return e.permissionMode
	}
	return ""
}

func (e *Evaluator) metadataFallback(req approvalmodel.Request) (approvalmodel.PolicyResult, bool) {
	metadata := mergeMetadata(req)
	// PermissionMode 已在 Evaluate 中处理；此处仅保留无 mode 时的 metadata 启发式。

	if metadata["critical"] == "true" || metadata["protected"] == "true" || metadata["scope"] == "protected" || metadata["workspace_metadata_write"] == "true" {
		return approvalmodel.PolicyResult{Type: approvalmodel.PolicyDeny, Reason: denyReason(metadata), Risk: approvalmodel.RiskCritical}, true
	}
	if metadata["trusted"] == "true" {
		return approvalmodel.PolicyResult{Type: approvalmodel.PolicyAllow, Reason: "trusted resource", Risk: riskOrDefault(req.Risk)}, true
	}
	if metadata["dangerous"] == "true" || metadata["destructive"] == "true" {
		return approvalmodel.PolicyResult{Type: decisionOf(e.defaults.Dangerous), Reason: "dangerous operation requires approval", Risk: approvalmodel.RiskHigh, SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce}}, true
	}
	if metadata["scope"] == "external" {
		return approvalmodel.PolicyResult{Type: approvalmodel.PolicyAsk, Reason: "external resource requires approval", Risk: approvalmodel.RiskHigh, SuggestedScopes: []approvalmodel.GrantScope{
			approvalmodel.GrantScopeOnce,
			approvalmodel.GrantScopeSession,
			approvalmodel.GrantScopeProject,
		}}, true
	}
	if metadata["scope"] == "workspace" {
		return approvalmodel.PolicyResult{Type: approvalmodel.PolicyAllow, Reason: "policy allow", Risk: riskOrDefault(req.Risk)}, true
	}
	// Skill 脚本执行：无 PermissionMode 时默认 ask，并建议 session 授权。
	if metadata["skill_script"] == "true" || req.ToolName == "run_skill_command" {
		return approvalmodel.PolicyResult{
			Type:            approvalmodel.PolicyAsk,
			Reason:          "skill script execution requires approval",
			Risk:            riskOrDefault(req.Risk),
			SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce, approvalmodel.GrantScopeSession},
		}, true
	}
	return approvalmodel.PolicyResult{}, false
}

func mergeMetadata(req approvalmodel.Request) map[string]string {
	metadata := make(map[string]string)
	for k, v := range req.Resource.Metadata {
		metadata[k] = v
	}
	for k, v := range req.Metadata {
		metadata[k] = v
	}
	return metadata
}

func denyReason(metadata map[string]string) string {
	if reason := metadata["deny_reason"]; reason != "" {
		return reason
	}
	return "operation denied by policy"
}
func (e *Evaluator) normalize(result approvalmodel.PolicyResult, req approvalmodel.Request) approvalmodel.PolicyResult {
	if result.Type == "" {
		result.Type = approvalmodel.PolicyAsk
	}
	if result.Risk == "" {
		result.Risk = riskOrDefault(req.Risk)
	}
	if result.Type == approvalmodel.PolicyAsk {
		result.SuggestedScopes = e.filterScopes(firstScopes(result.SuggestedScopes, req.SuggestedScopes))
	}
	return result
}

func (e *Evaluator) filterScopes(scopes []approvalmodel.GrantScope) []approvalmodel.GrantScope {
	allowed := map[approvalmodel.GrantScope]bool{}
	for _, scope := range e.defaults.AllowedGrantScopes {
		switch approvalmodel.GrantScope(strings.ToLower(strings.TrimSpace(scope))) {
		case approvalmodel.GrantScopeOnce, approvalmodel.GrantScopeTurn, approvalmodel.GrantScopeSession, approvalmodel.GrantScopeProject:
			allowed[approvalmodel.GrantScope(strings.ToLower(strings.TrimSpace(scope)))] = true
		}
	}
	if len(allowed) == 0 {
		allowed[approvalmodel.GrantScopeOnce] = true
	}
	out := make([]approvalmodel.GrantScope, 0, len(scopes))
	seen := map[approvalmodel.GrantScope]bool{}
	for _, scope := range scopes {
		if !allowed[scope] || seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	if len(out) == 0 && allowed[approvalmodel.GrantScopeOnce] {
		out = append(out, approvalmodel.GrantScopeOnce)
	}
	return out
}

func firstScopes(values ...[]approvalmodel.GrantScope) []approvalmodel.GrantScope {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce}
}

func decisionOf(value string) approvalmodel.PolicyType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow":
		return approvalmodel.PolicyAllow
	case "deny":
		return approvalmodel.PolicyDeny
	default:
		return approvalmodel.PolicyAsk
	}
}

func riskOrDefault(risk approvalmodel.RiskLevel) approvalmodel.RiskLevel {
	if risk == "" {
		return approvalmodel.RiskHigh
	}
	return risk
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
