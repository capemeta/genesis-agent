// Package contextutil 提供 context 属性提取辅助函数，用于隔离多租户和会话上下文。
package contextutil

import "context"

type tenantKey struct{}
type userKey struct{}
type sessionKey struct{}
type sandboxProfileKey struct{}

var (
	tenantIDKey        = tenantKey{}
	userIDKey          = userKey{}
	sessionIDKey       = sessionKey{}
	sandboxOverrideKey = sandboxProfileKey{}
)

// WithTenantID 将租户 ID 注入 context
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

// GetTenantID 从 context 提取租户 ID
func GetTenantID(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(tenantIDKey).(string)
	return val, ok
}

// WithUserID 将用户 ID 注入 context
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// GetUserID 从 context 提取用户 ID
func GetUserID(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(userIDKey).(string)
	return val, ok
}

// WithSessionID 将会话 ID 注入 context
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

// GetSessionID 从 context 提取会话 ID
func GetSessionID(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(sessionIDKey).(string)
	return val, ok
}

// WithSandboxProfileOverride 将会话级 sandbox 执行意图注入 context。
func WithSandboxProfileOverride(ctx context.Context, profile any) context.Context {
	return context.WithValue(ctx, sandboxOverrideKey, profile)
}

// GetSandboxProfileOverride 从 context 提取会话级 sandbox 执行意图。
func GetSandboxProfileOverride(ctx context.Context) (any, bool) {
	val := ctx.Value(sandboxOverrideKey)
	if val == nil {
		return nil, false
	}
	return val, true
}
