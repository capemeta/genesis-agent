package contract

import "context"

// ApprovalDecision project 来源 server 预连接审批结果。
type ApprovalDecision string

const (
	ApprovalPending  ApprovalDecision = "pending"
	ApprovalApproved ApprovalDecision = "approved"
	ApprovalRejected ApprovalDecision = "rejected"
)

// ApprovalStore 持久化 project 来源 server 的预连接审批状态。
type ApprovalStore interface {
	Get(ctx context.Context, serverName string) (ApprovalDecision, bool, error)
	Put(ctx context.Context, serverName string, decision ApprovalDecision) error
}
