package contract

import (
	"context"

	"genesis-agent/internal/capabilities/plan/model"
)

// Service 计划应用服务契约接口
type Service interface {
	// GetPlan 获取指定会话的当前计划
	GetPlan(ctx context.Context, sessionID string) (*model.Plan, error)
	// UpdatePlan 全量更新/重构计划大纲
	UpdatePlan(ctx context.Context, sessionID string, steps []model.Step, explanation string, operator string) (*model.Plan, error)
	// UpdateStepStatus 差量局部更改单个步骤状态（极低推理延迟）
	UpdateStepStatus(ctx context.Context, sessionID string, stepID string, status model.StepStatus, explanation string, operator string) (*model.Plan, error)
	// GeneratePromptReminder 生成供 runtime 注入的未完成步骤提醒
	GeneratePromptReminder(ctx context.Context, sessionID string, currentStep int) (string, bool, error)
}
