// Package trace 定义运行追踪契约。
package trace

import (
	"context"
	"time"
)

// Span 表示一个追踪跨度，记录某个操作的开始和结束。
type Span struct {
	SpanID    string
	Operation string
	StartedAt time.Time
	Tags      map[string]string
}

// Tracer 追踪器接口。
type Tracer interface {
	StartSpan(ctx context.Context, operation, id string) *Span
	EndSpan(ctx context.Context, span *Span, err error)
}
