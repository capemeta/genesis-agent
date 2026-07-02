// Package service 提供通用审批编排服务。
package service

import (
	"context"
	"fmt"

	"genesis-agent/internal/capabilities/approval/contract"
	"genesis-agent/internal/capabilities/approval/model"
)

// Service 编排 policy、cache 和 requester。
type Service struct {
	policy    contract.PolicyEngine
	requester contract.Requester
	store     contract.Store
}

// New 创建通用审批服务。
func New(policy contract.PolicyEngine, requester contract.Requester, store contract.Store) (*Service, error) {
	if policy == nil {
		return nil, fmt.Errorf("Approval PolicyEngine未配置")
	}
	if requester == nil {
		return nil, fmt.Errorf("Approval Requester未配置")
	}
	return &Service{policy: policy, requester: requester, store: store}, nil
}

// Authorize 执行审批授权。
func (s *Service) Authorize(ctx context.Context, req model.Request) (model.Decision, error) {
	if err := ctx.Err(); err != nil {
		return model.Decision{}, err
	}

	result, err := s.policy.Evaluate(ctx, req)
	if err != nil {
		return model.Decision{}, err
	}
	switch result.Type {
	case model.PolicyAllow:
		return model.Decision{Type: model.DecisionApproved, Scope: model.GrantScopeOnce, Reason: result.Reason}, nil
	case model.PolicyDeny:
		return model.Decision{Type: model.DecisionDenied, Scope: model.GrantScopeOnce, Reason: result.Reason}, nil
	case model.PolicyAsk:
		if decision, ok, err := s.cached(ctx, req); err != nil {
			return model.Decision{}, err
		} else if ok {
			return decision, nil
		}

		decision, err := s.requester.RequestApproval(ctx, req, result)
		if err != nil {
			return model.Decision{}, err
		}
		if err := s.remember(ctx, req, decision); err != nil {
			return model.Decision{}, err
		}
		return decision, nil
	default:
		return model.Decision{Type: model.DecisionDenied, Scope: model.GrantScopeOnce, Reason: "unknown approval policy result"}, nil
	}
}

func (s *Service) cached(ctx context.Context, req model.Request) (model.Decision, bool, error) {
	if s.store == nil {
		return model.Decision{}, false, nil
	}
	return s.store.Get(ctx, model.KeyFor(req, model.GrantScopeSession))
}

func (s *Service) remember(ctx context.Context, req model.Request, decision model.Decision) error {
	if s.store == nil || decision.Type != model.DecisionApprovedForScope || decision.Scope != model.GrantScopeSession {
		return nil
	}
	return s.store.Put(ctx, model.KeyFor(req, model.GrantScopeSession), decision)
}
