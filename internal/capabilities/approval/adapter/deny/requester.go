// Package deny 提供非交互审批 requester。
package deny

import (
	"context"

	"genesis-agent/internal/capabilities/approval/model"
)

// Requester 对所有 ask 返回 denied。
type Requester struct{}

// NewRequester 创建非交互 requester。
func NewRequester() *Requester { return &Requester{} }

// RequestApproval 返回需要确认但当前产品未启用交互确认。
func (r *Requester) RequestApproval(ctx context.Context, _ model.Request, _ model.PolicyResult) (model.Decision, error) {
	if err := ctx.Err(); err != nil {
		return model.Decision{}, err
	}
	return model.Decision{
		Type:   model.DecisionDenied,
		Scope:  model.GrantScopeOnce,
		Reason: "该操作需要用户确认，当前产品未启用交互确认",
	}, nil
}
