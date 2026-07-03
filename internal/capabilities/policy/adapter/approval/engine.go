// Package approval 将统一 policy evaluator 适配为现有 approval PolicyEngine。
package approval

import (
	"context"
	"fmt"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	policycontract "genesis-agent/internal/capabilities/policy/contract"
)

// Engine 实现 approvalcontract.PolicyEngine。
type Engine struct {
	evaluator policycontract.Evaluator
}

// NewEngine 创建 approval policy adapter。
func NewEngine(evaluator policycontract.Evaluator) (*Engine, error) {
	if evaluator == nil {
		return nil, fmt.Errorf("PolicyEvaluator未配置")
	}
	return &Engine{evaluator: evaluator}, nil
}

// Evaluate 将 approval request 交给统一 policy evaluator。
func (e *Engine) Evaluate(ctx context.Context, req approvalmodel.Request) (approvalmodel.PolicyResult, error) {
	if e == nil || e.evaluator == nil {
		return approvalmodel.PolicyResult{Type: approvalmodel.PolicyDeny, Reason: "policy evaluator not configured", Risk: approvalmodel.RiskCritical}, nil
	}
	return e.evaluator.Evaluate(ctx, req)
}
