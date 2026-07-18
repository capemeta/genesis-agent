// Package contract 定义 Agent App 有效配置解析端口。
package contract

import (
	"context"

	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ResolveRequest 只携带已认证作用域和 App 选择，不允许调用方提交权限配置。
type ResolveRequest struct {
	AppID string
	Scope workmodel.ResourceScope
}

// Resolver 由产品装配，返回已经完成配置合并和策略收窄的只读有效快照。
type Resolver interface {
	ResolveEffective(ctx context.Context, req ResolveRequest) (agentappmodel.EffectiveProfile, error)
}
