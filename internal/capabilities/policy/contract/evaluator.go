// Package contract 定义统一策略能力端口。
package contract

import (
	"context"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
)

// Evaluator 评估一次审批请求的统一策略结果。
type Evaluator interface {
	Evaluate(ctx context.Context, req approvalmodel.Request) (approvalmodel.PolicyResult, error)
}

// Matcher 判断某一类资源或动作是否匹配策略规则。
type Matcher interface {
	Match(ctx context.Context, req approvalmodel.Request) (approvalmodel.PolicyResult, bool, error)
}
