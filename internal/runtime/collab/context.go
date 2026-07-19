package collab

import (
	"context"
	"fmt"
	"strings"
)

type modeKey struct{}
type storeKey struct{}
type handoffKey struct{}

// WithMode 将当前协作模式注入 context（本 Run 有效）。
func WithMode(ctx context.Context, mode Mode) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, modeKey{}, Normalize(mode))
}

// ModeFromContext 读取协作模式；缺失则为 default。
func ModeFromContext(ctx context.Context) Mode {
	if ctx == nil {
		return ModeDefault
	}
	if m, ok := ctx.Value(modeKey{}).(Mode); ok {
		return Normalize(m)
	}
	return ModeDefault
}

// WithStore 注入 ModeStore，供工具 enter/exit 写回。
func WithStore(ctx context.Context, store Store) context.Context {
	if ctx == nil || store == nil {
		return ctx
	}
	return context.WithValue(ctx, storeKey{}, store)
}

// StoreFromContext 读取 ModeStore。
func StoreFromContext(ctx context.Context) (Store, bool) {
	if ctx == nil {
		return nil, false
	}
	s, ok := ctx.Value(storeKey{}).(Store)
	return s, ok && s != nil
}

// WithHandoffPending 标记本 Run 需要注入退出交接 reminder（通常来自上一轮批准）。
func WithHandoffPending(ctx context.Context, pending bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, handoffKey{}, pending)
}

// HandoffPendingFromContext 是否需要 handoff reminder。
func HandoffPendingFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, ok := ctx.Value(handoffKey{}).(bool)
	return ok && v
}

// ResolveSessionID 优先用显式 sessionID，否则尝试从调用方传入的 fallback。
func ResolveSessionID(sessionID, fallback string) string {
	if s := strings.TrimSpace(sessionID); s != "" {
		return s
	}
	return strings.TrimSpace(fallback)
}

// SessionMode 优先从 Store 读取会话模式（同轮 enter 后立刻生效）；无 Store 时回退 ModeFromContext。
// Store.Get 失败时返回错误（fail-closed），调用方不得静默当作 default。
func SessionMode(ctx context.Context, sessionID string) (Mode, error) {
	if store, ok := StoreFromContext(ctx); ok {
		if strings.TrimSpace(sessionID) == "" {
			return ModeDefault, fmt.Errorf("协作模式：缺少 session_id")
		}
		st, err := store.Get(ctx, sessionID)
		if err != nil {
			return ModeDefault, fmt.Errorf("读取协作模式失败: %w", err)
		}
		return Normalize(st.Mode), nil
	}
	return ModeFromContext(ctx), nil
}
