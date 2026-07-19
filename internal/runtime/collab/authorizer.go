package collab

import (
	"context"
	"fmt"

	"genesis-agent/internal/capabilities/tool/gateway"
	"genesis-agent/internal/platform/contextutil"
	multicontract "genesis-agent/internal/runtime/multiagent/contract"
)

// Authorizer 在 Gateway 层按协作模式拒绝越权工具（双保险，防可见性过滤被绕过）。
type Authorizer struct{}

// NewAuthorizer 创建协作模式 Authorizer。
func NewAuthorizer() gateway.Authorizer {
	return Authorizer{}
}

// AuthorizeTool 实现 gateway.Authorizer。
// 优先以 Store 中的会话模式为准，以便同轮 enter_plan_mode 后立刻放行 write_implementation_plan。
// Store 可读但 Get 失败时 fail-closed（拒绝），避免静默落到 default 放行变更工具。
func (Authorizer) AuthorizeTool(ctx context.Context, request gateway.AuthorizationRequest) (gateway.AuthorizationDecision, error) {
	mode := ModeFromContext(ctx)
	if store, ok := StoreFromContext(ctx); ok {
		sid, ok := contextutil.GetSessionID(ctx)
		if !ok || sid == "" {
			return gateway.AuthorizationDecision{
				Allowed: false,
				Reason:  "协作模式：缺少 session_id，拒绝工具执行",
			}, nil
		}
		st, err := store.Get(ctx, sid)
		if err != nil {
			return gateway.AuthorizationDecision{
				Allowed: false,
				Reason:  fmt.Sprintf("协作模式状态不可用，拒绝工具 %s: %v", request.ToolName, err),
			}, nil
		}
		mode = Normalize(st.Mode)
	}
	depth := multicontract.DelegationDepth(ctx)
	if ToolAllowed(mode, depth, request.ToolName) {
		return gateway.AuthorizationDecision{Allowed: true}, nil
	}
	return gateway.AuthorizationDecision{
		Allowed: false,
		Reason:  fmt.Sprintf("工具 %s 在当前协作模式(%s)或子智能体上下文中不可用", request.ToolName, DisplayName(mode)),
	}, nil
}
