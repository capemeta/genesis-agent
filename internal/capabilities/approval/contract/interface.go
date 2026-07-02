// Package contract 定义通用审批能力端口。
package contract

import (
	"context"

	"genesis-agent/internal/capabilities/approval/model"
)

// Service 是通用审批服务。
type Service interface {
	Authorize(ctx context.Context, req model.Request) (model.Decision, error)
}

// PolicyEngine 负责策略评估。
type PolicyEngine interface {
	Evaluate(ctx context.Context, req model.Request) (model.PolicyResult, error)
}

// Requester 负责把 ask 决策交给产品侧确认。
type Requester interface {
	RequestApproval(ctx context.Context, req model.Request, result model.PolicyResult) (model.Decision, error)
}

// Store 保存会话或更大范围的授权缓存。
type Store interface {
	Get(ctx context.Context, key model.GrantKey) (model.Decision, bool, error)
	Put(ctx context.Context, key model.GrantKey, decision model.Decision) error
}
