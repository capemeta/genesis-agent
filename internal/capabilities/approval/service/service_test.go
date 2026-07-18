package service

import (
	"context"
	"errors"
	"testing"

	"genesis-agent/internal/capabilities/approval/adapter/memory"
	"genesis-agent/internal/capabilities/approval/contract"
	"genesis-agent/internal/capabilities/approval/model"
	auditmodel "genesis-agent/internal/capabilities/audit/model"
	"genesis-agent/internal/platform/contextutil"
)

func TestAuthorizeAskApprovedNotifiesHook(t *testing.T) {
	requester := &fakeRequester{decision: model.Decision{Type: model.DecisionApproved, Scope: model.GrantScopeOnce}}
	svc, err := New(fakePolicy{result: model.PolicyResult{Type: model.PolicyAsk, Reason: "need"}}, requester, memory.NewStore(), nil)
	if err != nil {
		t.Fatal(err)
	}
	notified := false
	ctx := contextutil.WithApprovalGrantedHook(context.Background(), func(context.Context) {
		notified = true
	})
	decision, err := svc.Authorize(ctx, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != model.DecisionApproved {
		t.Fatalf("decision=%q", decision.Type)
	}
	if !notified {
		t.Fatal("expected ApprovalGrantedHook to fire")
	}
}

func TestAuthorizeAllowDoesNotCallRequester(t *testing.T) {
	svc, err := New(fakePolicy{result: model.PolicyResult{Type: model.PolicyAllow}}, &fakeRequester{}, memory.NewStore(), nil)
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

func TestAuthorizeWritesAuditWithRunID(t *testing.T) {
	audit := &captureAudit{}
	svc, err := New(fakePolicy{result: model.PolicyResult{Type: model.PolicyAllow, Reason: "ok"}}, &fakeRequester{}, memory.NewStore(), nil, WithAuditSink(audit))
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "run-approve-1")
	ctx = contextutil.WithSessionID(ctx, "sess-1")
	if _, err := svc.Authorize(ctx, testRequest()); err != nil {
		t.Fatal(err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	ev := audit.events[0]
	if ev.Category != "approval.decision" || ev.RunID != "run-approve-1" || ev.SessionID != "sess-1" || !ev.Allowed {
		t.Fatalf("event = %+v", ev)
	}
}

func TestAuthorizeDenyDoesNotCallRequester(t *testing.T) {
	requester := &fakeRequester{}
	svc, err := New(fakePolicy{result: model.PolicyResult{Type: model.PolicyDeny, Reason: "no"}}, requester, memory.NewStore(), nil)
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
	svc, err := New(policy, requester, memory.NewStore(), nil)
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
	svc, err := New(policy, requester, memory.NewStore(), nil)
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
	svc, err := New(fakePolicy{result: model.PolicyResult{Type: model.PolicyAsk}}, &fakeRequester{decision: model.Decision{Type: model.DecisionAbort}}, memory.NewStore(), nil)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := svc.Authorize(context.Background(), testRequest())
	if !errors.Is(err, contract.ErrRunAborted) {
		t.Fatalf("err = %v, want ErrRunAborted", err)
	}
	if decision.Type != model.DecisionAbort {
		t.Fatalf("decision = %q, want abort", decision.Type)
	}
}

func TestAuthorizeDoesNotCacheProjectScopeInFirstRound(t *testing.T) {
	requester := &fakeRequester{decision: model.Decision{Type: model.DecisionApprovedForScope, Scope: model.GrantScopeProject}}
	policy := &countingPolicy{result: model.PolicyResult{Type: model.PolicyAsk}}
	svc, err := New(policy, requester, memory.NewStore(), nil)
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

type captureAudit struct {
	events []auditmodel.Event
}

func (c *captureAudit) Record(_ context.Context, event auditmodel.Event) error {
	c.events = append(c.events, event)
	return nil
}
