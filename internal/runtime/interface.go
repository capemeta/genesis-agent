// package runtime 定义 Run Engine 接口
// Run Engine 是整个Agent系统的执行内核，负责驱动Loop策略运行
// 对应 AGENTS.md §4.1 RunEngine接口
package runtime

import (
	"context"

	"genesis-agent/internal/domain"
)

// RunEngine Run执行引擎接口
// 不同的Loop策略（ReAct / Plan-Execute / Coding等）实现此接口
type RunEngine interface {
	// Start 启动一次新的Run，同步执行直到完成或失败
	// 返回完整的Run记录（含所有Step）
	Start(ctx context.Context, req domain.StartRunRequest) (*domain.Run, error)

	// GetStrategyName 返回当前引擎使用的策略名称（用于日志/追踪）
	GetStrategyName() string
}
