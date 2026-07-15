package gateway

import "context"

// ChainAuthorizer 按顺序委托多个 Authorizer；任一拒绝则拒绝。
// 用于组合产品级 Authorizer 与 MCP Authorizer，避免 SetAuthorizer 覆盖。
type ChainAuthorizer struct {
	Authorizers []Authorizer
}

// NewChainAuthorizer 组装授权链，自动跳过 nil。
func NewChainAuthorizer(authorizers ...Authorizer) Authorizer {
	filtered := make([]Authorizer, 0, len(authorizers))
	for _, a := range authorizers {
		if a != nil {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return ChainAuthorizer{Authorizers: filtered}
}

func (c ChainAuthorizer) AuthorizeTool(ctx context.Context, request AuthorizationRequest) (AuthorizationDecision, error) {
	for _, a := range c.Authorizers {
		decision, err := a.AuthorizeTool(ctx, request)
		if err != nil {
			return AuthorizationDecision{}, err
		}
		if !decision.Allowed {
			return decision, nil
		}
	}
	return AuthorizationDecision{Allowed: true}, nil
}
