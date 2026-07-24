// Package contextutil 提供 context 属性提取辅助函数，用于隔离多租户和会话上下文。
package contextutil

import (
	"context"
	"strings"
)

type tenantKey struct{}
type userKey struct{}
type sessionKey struct{}
type runKey struct{}
type sandboxProfileKey struct{}
type approvalGrantedHookKey struct{}
type subagentTypeKeyType struct{}

var (
	tenantIDKey           = tenantKey{}
	userIDKey             = userKey{}
	sessionIDKey          = sessionKey{}
	runIDKey              = runKey{}
	sandboxOverrideKey    = sandboxProfileKey{}
	approvalGrantedHookID = approvalGrantedHookKey{}
	subagentTypeKey       = subagentTypeKeyType{}
)

// WithSubagentType 将子智能体类型注入 context（供 promptAudience 等识别角色）。
func WithSubagentType(ctx context.Context, subagentType string) context.Context {
	return context.WithValue(ctx, subagentTypeKey, strings.TrimSpace(subagentType))
}

// GetSubagentType 从 context 提取子智能体类型；空值或未设置时返回 ""。
func GetSubagentType(ctx context.Context) string {
	val, _ := ctx.Value(subagentTypeKey).(string)
	return strings.TrimSpace(val)
}


// ApprovalGrantedHook 在用户批准后回调（供 Repeat Guard 等 Run 级状态清零）。
type ApprovalGrantedHook func(ctx context.Context)

// WithApprovalGrantedHook 注入审批通过钩子。
func WithApprovalGrantedHook(ctx context.Context, hook ApprovalGrantedHook) context.Context {
	if ctx == nil || hook == nil {
		return ctx
	}
	return context.WithValue(ctx, approvalGrantedHookID, hook)
}

// NotifyApprovalGranted 在审批通过时触发钩子（无钩子则静默）。
func NotifyApprovalGranted(ctx context.Context) {
	if ctx == nil {
		return
	}
	hook, ok := ctx.Value(approvalGrantedHookID).(ApprovalGrantedHook)
	if !ok || hook == nil {
		return
	}
	hook(ctx)
}

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

// WithRunID 将一次 Agent Run ID 注入 context（跨 agent/audit/usage 关联主键）。
func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDKey, strings.TrimSpace(runID))
}

// GetRunID 从 context 提取 Run ID。
func GetRunID(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(runIDKey).(string)
	val = strings.TrimSpace(val)
	if !ok || val == "" {
		return "", false
	}
	return val, true
}

// CorrelationIDs 返回日志关联键；缺失时返回空字符串。
func CorrelationIDs(ctx context.Context) (runID, sessionID string) {
	if ctx == nil {
		return "", ""
	}
	if id, ok := GetRunID(ctx); ok {
		runID = id
	}
	if id, ok := GetSessionID(ctx); ok {
		sessionID = strings.TrimSpace(id)
	}
	return runID, sessionID
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
