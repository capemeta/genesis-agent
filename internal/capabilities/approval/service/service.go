// Package service 提供通用审批编排服务。
package service

import (
	"context"
	"fmt"
	"time"

	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	auditmodel "genesis-agent/internal/capabilities/audit/model"
	"genesis-agent/internal/capabilities/approval/contract"
	"genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/platform/logger/correl"
)

// Service 编排 policy、cache 和 requester。
type Service struct {
	policy    contract.PolicyEngine
	requester contract.Requester
	store     contract.Store
	log       logger.Logger
	audit     auditcontract.Sink
}

// Option 调整审批服务可选依赖。
type Option func(*Service)

// WithAuditSink 注入审计 Sink，将审批决策写入 audit 通道。
func WithAuditSink(sink auditcontract.Sink) Option {
	return func(s *Service) {
		s.audit = sink
	}
}

// New 创建通用审批服务。
func New(policy contract.PolicyEngine, requester contract.Requester, store contract.Store, log logger.Logger, opts ...Option) (*Service, error) {
	if policy == nil {
		return nil, fmt.Errorf("Approval PolicyEngine未配置")
	}
	if requester == nil {
		return nil, fmt.Errorf("Approval Requester未配置")
	}
	if log == nil {
		log = logger.NewNop()
	}
	svc := &Service{policy: policy, requester: requester, store: store, log: log}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc, nil
}

// Authorize 执行审批授权并打印安全流转日志。
func (s *Service) Authorize(ctx context.Context, req model.Request) (model.Decision, error) {
	if err := ctx.Err(); err != nil {
		return model.Decision{}, err
	}

	l := correl.AttachLogger(ctx, s.log).With("tool", req.ToolName, "action", string(req.Action), "resource", req.Resource.Display)
	l.Info("开始执行安全审批评估")

	result, err := s.policy.Evaluate(ctx, req)
	if err != nil {
		l.Error("安全策略评估失败", "error", err)
		s.recordAudit(ctx, req, model.Decision{Type: model.DecisionDenied, Scope: model.GrantScopeOnce, Reason: err.Error()}, false, err.Error())
		return model.Decision{}, err
	}
	switch result.Type {
	case model.PolicyAllow:
		l.Info("安全策略评估结果: [直接放行]")
		decision := model.Decision{Type: model.DecisionApproved, Scope: model.GrantScopeOnce, Reason: result.Reason}
		s.recordAudit(ctx, req, decision, true, result.Reason)
		return decision, nil
	case model.PolicyDeny:
		l.Warn("安全策略评估结果: [直接拒绝]", "reason", result.Reason)
		decision := model.Decision{Type: model.DecisionDenied, Scope: model.GrantScopeOnce, Reason: result.Reason}
		s.recordAudit(ctx, req, decision, false, result.Reason)
		return decision, nil
	case model.PolicyAsk:
		if decision, ok, err := s.cached(ctx, req); err != nil {
			l.Error("读取安全授权缓存失败", "error", err)
			return model.Decision{}, err
		} else if ok {
			l.Info("命中会话级授权缓存，自动允许执行", "decision", string(decision.Type), "scope", string(decision.Scope))
			s.recordAudit(ctx, req, decision, decision.Type == model.DecisionApproved || decision.Type == model.DecisionApprovedForScope, "cached:"+decision.Reason)
			return decision, nil
		}

		l.Warn("安全策略评估结果: [需要人机交互授权]，进入挂起等待...", "reason", result.Reason)
		decision, err := s.requester.RequestApproval(ctx, req, result)
		if err != nil {
			l.Error("人机交互审批失败", "error", err)
			s.recordAudit(ctx, req, model.Decision{Type: model.DecisionDenied, Scope: model.GrantScopeOnce, Reason: err.Error()}, false, err.Error())
			return model.Decision{}, err
		}
		l.Info("人工干预决策已返回", "decision", string(decision.Type), "scope", string(decision.Scope), "reason", decision.Reason)
		if err := s.remember(ctx, req, decision); err != nil {
			l.Error("写入会话级授权缓存失败", "error", err)
			return model.Decision{}, err
		}
		s.recordAudit(ctx, req, decision, decision.Type == model.DecisionApproved || decision.Type == model.DecisionApprovedForScope, decision.Reason)
		if decision.Type == model.DecisionApproved || decision.Type == model.DecisionApprovedForScope {
			contextutil.NotifyApprovalGranted(ctx)
		}
		return decision, nil
	default:
		l.Error("未知安全策略评估结果，默认拒绝", "policy_type", string(result.Type))
		decision := model.Decision{Type: model.DecisionDenied, Scope: model.GrantScopeOnce, Reason: "unknown approval policy result"}
		s.recordAudit(ctx, req, decision, false, decision.Reason)
		return decision, nil
	}
}

func (s *Service) recordAudit(ctx context.Context, req model.Request, decision model.Decision, allowed bool, reason string) {
	if s == nil || s.audit == nil {
		return
	}
	now := time.Now()
	runID, sessionID, metadata := correl.Enrich(ctx, "", "", map[string]string{
		"tool":          req.ToolName,
		"action":        string(req.Action),
		"decision":      string(decision.Type),
		"grant_scope":   string(decision.Scope),
		"resource":      firstNonEmpty(req.Resource.Display, req.Resource.URI),
		"resource_type": req.Resource.Type,
	})
	_ = s.audit.Record(ctx, auditmodel.Event{
		Category:    "approval.decision",
		Action:      string(req.Action),
		Resource:    firstNonEmpty(req.Resource.Display, req.Resource.URI, req.ToolName),
		RunID:       runID,
		SessionID:   sessionID,
		Severity:    auditSeverity(allowed),
		Allowed:     allowed,
		Reason:      reason,
		StartedAt:   now,
		CompletedAt: now,
		Metadata:    metadata,
	})
}

func auditSeverity(allowed bool) auditmodel.Severity {
	if allowed {
		return auditmodel.SeverityInfo
	}
	return auditmodel.SeverityWarn
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
