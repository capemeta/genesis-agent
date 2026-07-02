package service

import (
	"context"
	"testing"

	"genesis-agent/internal/capabilities/approval/adapter/memory"
	"genesis-agent/internal/capabilities/approval/model"
)

func TestAuthorizeAllowDoesNotCallRequester(t *testing.T) {
	svc, err := New(fakePolicy{result: model.PolicyResult{Type: model.PolicyAllow}}, &fakeRequester{}, memory.NewStore())
	if err != nil {
		t.Fatal(err)
	}
	decision, err := svc.Authorize(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != model.DecisionApproved {
		t.Fatalf("decision = %q, want approved", decision.Type)
	}
}

func TestAuthorizeDenyDoesNotCallRequester(t *testing.T) {
	requester := &fakeRequester{}
	svc, err := New(fakePolicy{result: model.PolicyResult{Type: model.PolicyDeny, Reason: "no"}}, requester, memory.NewStore())
	if err != nil {
		t.Fatal(err)
	}
	decision, err := svc.Authorize(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != model.DecisionDenied || requester.calls != 0 {
		t.Fatalf("decision=%q calls=%d, want denied and no requester call", decision.Type, requester.calls)
	}
}

func TestAuthorizeAskStoresSessionDecisionButStillEvaluatesPolicy(t *testing.T) {
	requester := &fakeRequester{decision: model.Decision{Type: model.DecisionApprovedForScope, Scope: model.GrantScopeSession}}
	policy := &countingPolicy{result: model.PolicyResult{Type: model.PolicyAsk}}
	svc, err := New(policy, requester, memory.NewStore())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authorize(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authorize(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	if requester.calls != 1 {
		t.Fatalf("requester calls = %d, want 1", requester.calls)
	}
	if policy.calls != 2 {
		t.Fatalf("policy calls = %d, want 2 because hard-deny policy must always be re-evaluated", policy.calls)
	}
}

func TestAuthorizeDenyOverridesCachedSessionGrant(t *testing.T) {
	requester := &fakeRequester{decision: model.Decision{Type: model.DecisionApprovedForScope, Scope: model.GrantScopeSession}}
	policy := &mutablePolicy{result: model.PolicyResult{Type: model.PolicyAsk}}
	svc, err := New(policy, requester, memory.NewStore())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authorize(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}

	policy.result = model.PolicyResult{Type: model.PolicyDeny, Reason: "hard deny"}
	decision, err := svc.Authorize(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != model.DecisionDenied {
		t.Fatalf("decision = %q, want deny", decision.Type)
	}
	if requester.calls != 1 {
		t.Fatalf("requester calls = %d, want 1", requester.calls)
	}
}

func TestAuthorizeAbortDecisionReturned(t *testing.T) {
	svc, err := New(fakePolicy{result: model.PolicyResult{Type: model.PolicyAsk}}, &fakeRequester{decision: model.Decision{Type: model.DecisionAbort}}, memory.NewStore())
	if err != nil {
		t.Fatal(err)
	}
	decision, err := svc.Authorize(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != model.DecisionAbort {
		t.Fatalf("decision = %q, want abort", decision.Type)
	}
}

func TestAuthorizeDoesNotCacheProjectScopeInFirstRound(t *testing.T) {
	requester := &fakeRequester{decision: model.Decision{Type: model.DecisionApprovedForScope, Scope: model.GrantScopeProject}}
	policy := &countingPolicy{result: model.PolicyResult{Type: model.PolicyAsk}}
	svc, err := New(policy, requester, memory.NewStore())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authorize(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authorize(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	if requester.calls != 2 {
		t.Fatalf("requester calls = %d, want 2 because project scope is not implemented in first round", requester.calls)
	}
}

type fakePolicy struct{ result model.PolicyResult }

func (p fakePolicy) Evaluate(context.Context, model.Request) (model.PolicyResult, error) {
	return p.result, nil
}

type countingPolicy struct {
	result model.PolicyResult
	calls  int
}

func (p *countingPolicy) Evaluate(context.Context, model.Request) (model.PolicyResult, error) {
	p.calls++
	return p.result, nil
}

type mutablePolicy struct{ result model.PolicyResult }

func (p *mutablePolicy) Evaluate(context.Context, model.Request) (model.PolicyResult, error) {
	return p.result, nil
}

type fakeRequester struct {
	decision model.Decision
	calls    int
}

func (r *fakeRequester) RequestApproval(context.Context, model.Request, model.PolicyResult) (model.Decision, error) {
	r.calls++
	if r.decision.Type == "" {
		return model.Decision{Type: model.DecisionDenied}, nil
	}
	return r.decision, nil
}

func testRequest() model.Request {
	return model.Request{
		Action:   model.ActionFileWrite,
		Resource: model.Resource{Type: "file", URI: "workspace://a.txt"},
	}
}
