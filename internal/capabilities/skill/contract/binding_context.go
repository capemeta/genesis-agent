package contract

import (
	"context"

	"genesis-agent/internal/capabilities/skill/model"
)

type invocationBindingContextKey struct{}
type invocationAncestorsContextKey struct{}

func WithInvocationBinding(ctx context.Context, binding model.InvocationBinding) context.Context {
	return context.WithValue(ctx, invocationBindingContextKey{}, binding.Clone())
}

func WithInvocationAncestors(ctx context.Context, ancestors []string) context.Context {
	return context.WithValue(ctx, invocationAncestorsContextKey{}, append([]string(nil), ancestors...))
}

func InvocationAncestors(ctx context.Context) []string {
	ancestors, _ := ctx.Value(invocationAncestorsContextKey{}).([]string)
	return append([]string(nil), ancestors...)
}

func InvocationBindingFromContext(ctx context.Context) (model.InvocationBinding, bool) {
	binding, ok := ctx.Value(invocationBindingContextKey{}).(model.InvocationBinding)
	if !ok || binding.ID == "" {
		return model.InvocationBinding{}, false
	}
	return binding.Clone(), true
}
