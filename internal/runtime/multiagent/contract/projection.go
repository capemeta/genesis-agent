package contract

import (
	"context"

	"genesis-agent/internal/runtime/multiagent/model"
)

// ProjectionSink 消费子智能体控制面事件，供 CLI/Desktop/Enterprise 做各自投影。
// 事件不得携带 transcript、原始工具输出、credential 或未重新鉴权的 artifact 内容。
type ProjectionSink interface {
	EmitProjection(ctx context.Context, event model.ProjectionEvent) error
}

// ProjectionReader 为产品投影提供经过归约的只读事件查询。
type ProjectionReader interface {
	ListProjectionEvents(ctx context.Context, query model.ProjectionQuery) ([]model.ProjectionEvent, error)
}
