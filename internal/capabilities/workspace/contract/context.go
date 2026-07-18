package contract

import (
	"context"

	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type preparedRunContextKey struct{}
type controlPlaneContextKey struct{}

// WithPreparedRun 仅供可信控制面把不可变准备结果传入执行链路。
func WithPreparedRun(ctx context.Context, prepared workmodel.PreparedRun) context.Context {
	return context.WithValue(ctx, preparedRunContextKey{}, prepared)
}

// PreparedRunFromContext 返回当前 Run 的控制面快照。
func PreparedRunFromContext(ctx context.Context) (workmodel.PreparedRun, bool) {
	value, ok := ctx.Value(preparedRunContextKey{}).(workmodel.PreparedRun)
	return value, ok
}

// WithControlPlane 注入只能由产品 bootstrap 构造的工作空间控制面。
func WithControlPlane(ctx context.Context, control ControlPlane) context.Context {
	return context.WithValue(ctx, controlPlaneContextKey{}, control)
}

// ControlPlaneFromContext 返回当前 Run 的派生 execution 控制面。
func ControlPlaneFromContext(ctx context.Context) (ControlPlane, bool) {
	value, ok := ctx.Value(controlPlaneContextKey{}).(ControlPlane)
	return value, ok && value != nil
}
