// Package prompt 定义运行时提示词构建接口与默认实现。
package prompt

import (
	"context"

	"genesis-agent/internal/domain"
)

// BuildRequest 描述一次系统提示词构建请求。
type BuildRequest struct {
	Agent   *domain.Agent
	Run     *domain.Run
	UserID  string
	TurnID  string
	Context map[string]string
}

// Fragment 是动态上下文片段。
type Fragment struct {
	Name     string
	Contents string
}

// ContextInjector 在运行时注入动态上下文，例如 Skills、记忆摘要等。
type ContextInjector interface {
	Inject(ctx context.Context, req BuildRequest) (Fragment, error)
}

// ContextInjectorFunc 让普通函数可作为注入器。
type ContextInjectorFunc func(ctx context.Context, req BuildRequest) (Fragment, error)

func (f ContextInjectorFunc) Inject(ctx context.Context, req BuildRequest) (Fragment, error) {
	return f(ctx, req)
}

// Builder 构建运行时提示词。
type Builder interface {
	BuildSystem(ctx context.Context, req BuildRequest) (string, error)
}
