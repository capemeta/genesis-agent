// Package static 提供默认审批策略。
package static

import (
	"context"

	"genesis-agent/internal/capabilities/approval/model"
)

// PolicyEngine 是基于请求 metadata 的默认策略引擎。
type PolicyEngine struct{}

// NewPolicyEngine 创建默认策略引擎。
func NewPolicyEngine() *PolicyEngine { return &PolicyEngine{} }

// Evaluate 评估请求。
func (e *PolicyEngine) Evaluate(ctx context.Context, req model.Request) (model.PolicyResult, error) {
	if err := ctx.Err(); err != nil {
		return model.PolicyResult{}, err
	}
	metadata := mergeMetadata(req)
	if metadata["critical"] == "true" || metadata["protected"] == "true" || metadata["scope"] == "protected" || metadata["workspace_metadata_write"] == "true" {
		return model.PolicyResult{Type: model.PolicyDeny, Reason: denyReason(metadata), Risk: model.RiskCritical}, nil
	}
	if metadata["trusted"] == "true" {
		return model.PolicyResult{Type: model.PolicyAllow, Reason: "trusted resource", Risk: riskOrDefault(req.Risk)}, nil
	}
	if metadata["dangerous"] == "true" || metadata["destructive"] == "true" {
		return askOnce("dangerous operation requires approval", model.RiskHigh), nil
	}
	if metadata["scope"] == "external" {
		return askScoped("external resource requires approval", model.RiskHigh, []model.GrantScope{
			model.GrantScopeOnce,
			model.GrantScopeSession,
			model.GrantScopeProject,
		}), nil
	}
	if metadata["scope"] == "workspace" {
		return model.PolicyResult{Type: model.PolicyAllow, Reason: "policy allow", Risk: riskOrDefault(req.Risk)}, nil
	}
	return askOnce("unclassified operation requires approval", model.RiskHigh), nil
}

func mergeMetadata(req model.Request) map[string]string {
	metadata := make(map[string]string)
	for k, v := range req.Resource.Metadata {
		metadata[k] = v
	}
	for k, v := range req.Metadata {
		metadata[k] = v
	}
	return metadata
}

func askOnce(reason string, risk model.RiskLevel) model.PolicyResult {
	return askScoped(reason, risk, []model.GrantScope{model.GrantScopeOnce})
}

func askScoped(reason string, risk model.RiskLevel, scopes []model.GrantScope) model.PolicyResult {
	return model.PolicyResult{
		Type:            model.PolicyAsk,
		Reason:          reason,
		Risk:            risk,
		SuggestedScopes: scopes,
	}
}

func denyReason(metadata map[string]string) string {
	if reason := metadata["deny_reason"]; reason != "" {
		return reason
	}
	return "operation denied by policy"
}

func riskOrDefault(risk model.RiskLevel) model.RiskLevel {
	if risk == "" {
		return model.RiskLow
	}
	return risk
}
