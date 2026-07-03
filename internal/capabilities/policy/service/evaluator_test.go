package service

import (
	"context"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/platform/config"
)

type fakeMatcher struct {
	result approvalmodel.PolicyResult
	ok     bool
}

func (m fakeMatcher) Match(context.Context, approvalmodel.Request) (approvalmodel.PolicyResult, bool, error) {
	return m.result, m.ok, nil
}

func TestEvaluatorDenyOverridesEarlierAllow(t *testing.T) {
	evaluator := NewEvaluator(config.PolicyDefaultsConfig{Unknown: "ask", AllowedGrantScopes: []string{"once", "session"}},
		fakeMatcher{ok: true, result: approvalmodel.PolicyResult{Type: approvalmodel.PolicyAllow}},
		fakeMatcher{ok: true, result: approvalmodel.PolicyResult{Type: approvalmodel.PolicyDeny, Reason: "no"}},
	)
	result, err := evaluator.Evaluate(context.Background(), approvalmodel.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != approvalmodel.PolicyDeny || result.Reason != "no" {
		t.Fatalf("result = %+v, want deny", result)
	}
}

func TestEvaluatorFiltersTenantGlobalScopes(t *testing.T) {
	evaluator := NewEvaluator(config.PolicyDefaultsConfig{Unknown: "ask", AllowedGrantScopes: []string{"once", "session"}},
		fakeMatcher{ok: true, result: approvalmodel.PolicyResult{Type: approvalmodel.PolicyAsk, SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeTenant, approvalmodel.GrantScopeSession, approvalmodel.GrantScopeGlobal}}},
	)
	result, err := evaluator.Evaluate(context.Background(), approvalmodel.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SuggestedScopes) != 1 || result.SuggestedScopes[0] != approvalmodel.GrantScopeSession {
		t.Fatalf("scopes = %+v, want session only", result.SuggestedScopes)
	}
}

func TestEvaluatorUsesUnknownDefault(t *testing.T) {
	evaluator := NewEvaluator(config.PolicyDefaultsConfig{Unknown: "deny", AllowedGrantScopes: []string{"once"}})
	result, err := evaluator.Evaluate(context.Background(), approvalmodel.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != approvalmodel.PolicyDeny {
		t.Fatalf("result = %+v, want deny", result)
	}
}

func TestEvaluatorMetadataFallbackAllowsTrustedResource(t *testing.T) {
	evaluator := NewEvaluator(config.PolicyDefaultsConfig{Unknown: "ask", Dangerous: "ask", AllowedGrantScopes: []string{"once", "session"}})
	result, err := evaluator.Evaluate(context.Background(), approvalmodel.Request{Metadata: map[string]string{"trusted": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != approvalmodel.PolicyAllow {
		t.Fatalf("result = %+v, want allow", result)
	}
}

func TestEvaluatorMetadataFallbackDeniesCritical(t *testing.T) {
	evaluator := NewEvaluator(config.PolicyDefaultsConfig{Unknown: "ask", Dangerous: "ask", AllowedGrantScopes: []string{"once", "session"}})
	result, err := evaluator.Evaluate(context.Background(), approvalmodel.Request{Metadata: map[string]string{"critical": "true", "deny_reason": "hard stop"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != approvalmodel.PolicyDeny || result.Reason != "hard stop" {
		t.Fatalf("result = %+v, want deny with reason", result)
	}
}

func TestEvaluatorMetadataFallbackAllowsWorkspace(t *testing.T) {
	evaluator := NewEvaluator(config.PolicyDefaultsConfig{Unknown: "ask", Dangerous: "ask", AllowedGrantScopes: []string{"once", "session"}})
	result, err := evaluator.Evaluate(context.Background(), approvalmodel.Request{Metadata: map[string]string{"scope": "workspace"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != approvalmodel.PolicyAllow {
		t.Fatalf("result = %+v, want allow", result)
	}
}
