package contract

import (
	"context"

	"genesis-agent/internal/capabilities/usage/model"
)

// Sink 接收用量事件。
type Sink interface {
	RecordToolUsage(ctx context.Context, usage model.ToolUsage) error
}
