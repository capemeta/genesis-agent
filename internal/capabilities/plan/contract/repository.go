// Package contract 定义 Plan 能力域对外的抽象契约接口。
package contract

import (
	"context"

	"genesis-agent/internal/capabilities/plan/model"
)

// Repository 计划存储契约接口（支持读写分离）
type Repository interface {
	// GetPlan 获取指定 Session 的最新计划快照
	GetPlan(ctx context.Context, sessionID string) (*model.Plan, error)
	// SavePlan 保存或更新计划快照，且需以 Append-only 方式追加审计变更日志
	SavePlan(ctx context.Context, plan *model.Plan, revision *model.RevisionLog) error
	// GetHistory 获取变更日志记录
	GetHistory(ctx context.Context, sessionID string) ([]model.RevisionLog, error)
}
