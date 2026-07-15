package contract

import (
	"context"

	"genesis-agent/internal/capabilities/hook/model"
)

// Runner 执行一种 handler 类型，并返回归一化决策。
type Runner interface {
	Kind() string
	Run(ctx context.Context, spec model.HandlerSpec, inputJSON []byte) model.Decision
}
