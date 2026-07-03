package contract

import (
	"context"

	"genesis-agent/internal/capabilities/audit/model"
)

// Sink 接收系统化审计事件。
type Sink interface {
	Record(ctx context.Context, event model.Event) error
}
