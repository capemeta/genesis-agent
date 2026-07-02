package action

import (
	"context"
	"genesis-agent/internal/domain"
)

// Executor 执行器接口，负责调度具体的工具、MCP 或子 Agent 动作
type Executor interface {
	Execute(ctx context.Context, step *domain.Step) (*domain.Step, error)
}
