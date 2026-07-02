// Package consoletrace 提供控制台追踪实现。
package consoletrace

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	contract "genesis-agent/internal/capabilities/trace/contract"
)

// consoleTracer 将追踪信息输出到控制台，用于开发调试。
type consoleTracer struct {
	mu    sync.Mutex
	spans map[string]*contract.Span
}

// NewConsoleTracer 创建控制台追踪器。
func NewConsoleTracer() contract.Tracer {
	return &consoleTracer{spans: make(map[string]*contract.Span)}
}

func (t *consoleTracer) StartSpan(_ context.Context, operation, id string) *contract.Span {
	span := &contract.Span{
		SpanID:    id,
		Operation: operation,
		StartedAt: time.Now(),
		Tags:      make(map[string]string),
	}

	t.mu.Lock()
	t.spans[id] = span
	t.mu.Unlock()

	fmt.Fprintf(os.Stdout, "[TRACE] > %-12s  id=%-30s  started\n", operation, id)
	return span
}

func (t *consoleTracer) EndSpan(_ context.Context, span *contract.Span, err error) {
	elapsed := time.Since(span.StartedAt)

	t.mu.Lock()
	delete(t.spans, span.SpanID)
	t.mu.Unlock()

	status := "OK"
	if err != nil {
		status = "ERROR: " + err.Error()
	}

	fmt.Fprintf(os.Stdout, "[TRACE] < %-12s  id=%-30s  elapsed=%-10v  status=%s\n",
		span.Operation, span.SpanID, elapsed.Round(time.Millisecond), status)
}

type nopTracer struct{}

// NewNopTracer 创建空追踪器。
func NewNopTracer() contract.Tracer { return &nopTracer{} }

func (n *nopTracer) StartSpan(_ context.Context, operation, id string) *contract.Span {
	return &contract.Span{SpanID: id, Operation: operation, StartedAt: time.Now(), Tags: map[string]string{}}
}

func (n *nopTracer) EndSpan(_ context.Context, _ *contract.Span, _ error) {}
