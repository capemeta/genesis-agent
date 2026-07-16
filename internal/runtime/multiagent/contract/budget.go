package contract

import (
	"context"
	"fmt"
	"sync"
)

type treeBudgetKey struct{}
type delegationDepthKey struct{}
type maxDelegationDepthKey struct{}
type delegationReadOnlyKey struct{}
type delegationToolsKey struct{}

// TreeBudget 是一次根委派及其后代共享的硬预算。所有计数均由可信运行时累加。
type TreeBudget struct {
	mu           sync.Mutex
	maxTokens    int64
	maxToolCalls int
	tokens       int64
	toolCalls    int
}

func NewTreeBudget(maxTokens int64, maxToolCalls int) *TreeBudget {
	return &TreeBudget{maxTokens: maxTokens, maxToolCalls: maxToolCalls}
}

func (b *TreeBudget) Consume(tokens int64, toolCalls int) error {
	if b == nil {
		return nil
	}
	if tokens < 0 || toolCalls < 0 {
		return fmt.Errorf("子智能体树预算增量不能为负数")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.maxTokens > 0 && b.tokens+tokens > b.maxTokens {
		return fmt.Errorf("subagent tree budget exceeded: tokens")
	}
	if b.maxToolCalls > 0 && b.toolCalls+toolCalls > b.maxToolCalls {
		return fmt.Errorf("subagent tree budget exceeded: tool_calls")
	}
	b.tokens += tokens
	b.toolCalls += toolCalls
	return nil
}

func WithTreeBudget(ctx context.Context, budget *TreeBudget) context.Context {
	return context.WithValue(ctx, treeBudgetKey{}, budget)
}

func TreeBudgetFromContext(ctx context.Context) *TreeBudget {
	budget, _ := ctx.Value(treeBudgetKey{}).(*TreeBudget)
	return budget
}

func WithDelegationDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, delegationDepthKey{}, depth)
}

func DelegationDepth(ctx context.Context) int {
	depth, _ := ctx.Value(delegationDepthKey{}).(int)
	return depth
}

func WithMaxDelegationDepth(ctx context.Context, maxDepth int) context.Context {
	return context.WithValue(ctx, maxDelegationDepthKey{}, maxDepth)
}

func MaxDelegationDepth(ctx context.Context) int {
	maxDepth, _ := ctx.Value(maxDelegationDepthKey{}).(int)
	return maxDepth
}

func WithDelegationReadOnly(ctx context.Context, readOnly bool) context.Context {
	return context.WithValue(ctx, delegationReadOnlyKey{}, readOnly)
}

func DelegationReadOnly(ctx context.Context) bool {
	readOnly, _ := ctx.Value(delegationReadOnlyKey{}).(bool)
	return readOnly
}

func WithDelegationTools(ctx context.Context, tools []string) context.Context {
	copyTools := append([]string(nil), tools...)
	return context.WithValue(ctx, delegationToolsKey{}, copyTools)
}

func DelegationTools(ctx context.Context) ([]string, bool) {
	tools, ok := ctx.Value(delegationToolsKey{}).([]string)
	return append([]string(nil), tools...), ok
}
