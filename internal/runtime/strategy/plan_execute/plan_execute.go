package planexecute

import (
	"context"

	"genesis-agent/internal/domain"
)

// PlanExecuteEngine Plan-Execute 引擎实现（Phase 2 核心）
type PlanExecuteEngine struct{}

// NewPlanExecuteEngine 创建 Plan-Execute 引擎
func NewPlanExecuteEngine() *PlanExecuteEngine {
	return &PlanExecuteEngine{}
}

// Start 启动一次 Plan-Execute 推理
func (e *PlanExecuteEngine) Start(ctx context.Context, req domain.StartRunRequest) (*domain.Run, error) {
	// TODO: Phase 2 实施
	return nil, nil
}

// GetStrategyName 获取策略名称
func (e *PlanExecuteEngine) GetStrategyName() string {
	return "plan_execute"
}
