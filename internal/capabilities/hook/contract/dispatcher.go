// Package contract 定义 Hook 能力域对运行时暴露的端口。
package contract

import (
	"context"
	"sync"

	"genesis-agent/internal/capabilities/hook/model"
)

// Dispatcher 是内核触发 Hook 的唯一入口。
type Dispatcher interface {
	Dispatch(ctx context.Context, event model.Event) (model.AggregateResult, error)
}

type dispatcherKey struct{}
type additionsKey struct{}
type scopeKey struct{}

type additions struct {
	mu    sync.Mutex
	items []string
}

// WithDispatcher 将 dispatcher 与本轮 Hook 上下文队列放进 context。
func WithDispatcher(ctx context.Context, dispatcher Dispatcher) context.Context {
	if dispatcher == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, dispatcherKey{}, dispatcher)
	return context.WithValue(ctx, additionsKey{}, &additions{})
}

// FromContext 取出 dispatcher；未注入时返回 nil，调用方应按 no-op 处理。
func FromContext(ctx context.Context) Dispatcher {
	if ctx == nil {
		return nil
	}
	dispatcher, _ := ctx.Value(dispatcherKey{}).(Dispatcher)
	return dispatcher
}

// WithScopeContext 注入本次运行的能力适用范围事实。
func WithScopeContext(ctx context.Context, scope model.ScopeContext) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// ScopeContextFromContext 返回当前运行的 scope 上下文。
func ScopeContextFromContext(ctx context.Context) model.ScopeContext {
	if ctx == nil {
		return model.ScopeContext{}
	}
	scope, _ := ctx.Value(scopeKey{}).(model.ScopeContext)
	return scope
}

// AppendAdditionalContext 把 Hook 产生的上下文安全地排入下一轮 prompt。
func AppendAdditionalContext(ctx context.Context, values ...string) {
	queue, _ := ctx.Value(additionsKey{}).(*additions)
	if queue == nil {
		return
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	for _, value := range values {
		if value != "" {
			queue.items = append(queue.items, value)
		}
	}
}

// DrainAdditionalContext 取走当前已积累的 Hook 上下文。
func DrainAdditionalContext(ctx context.Context) []string {
	queue, _ := ctx.Value(additionsKey{}).(*additions)
	if queue == nil {
		return nil
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	items := append([]string(nil), queue.items...)
	queue.items = nil
	return items
}
