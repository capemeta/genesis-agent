package contract

import (
	"context"

	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// CompletionDecision 是 workspace 侧完成门禁的确定性结论。
type CompletionDecision struct {
	Complete bool
	Reminder string
}

// RunCompletionGuard 记录 Run 开始前的资源基线，并在最终回答前验证增量。
type RunCompletionGuard interface {
	InitializeRun(ctx context.Context, prepared workmodel.PreparedRun) error
	EvaluateCompletion(ctx context.Context, prepared workmodel.PreparedRun) (CompletionDecision, error)
	// ReleaseRun 必须幂等，用于清理失败、取消和正常结束 Run 的临时基线。
	ReleaseRun(prepared workmodel.PreparedRun)
}

type completionGuardContextKey struct{}

func WithCompletionGuard(ctx context.Context, guard RunCompletionGuard) context.Context {
	if guard == nil {
		return ctx
	}
	return context.WithValue(ctx, completionGuardContextKey{}, guard)
}

func CompletionGuardFromContext(ctx context.Context) (RunCompletionGuard, bool) {
	if ctx == nil {
		return nil, false
	}
	guard, ok := ctx.Value(completionGuardContextKey{}).(RunCompletionGuard)
	return guard, ok && guard != nil
}
