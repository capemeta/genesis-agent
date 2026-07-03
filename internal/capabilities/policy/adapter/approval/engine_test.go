package approval

import (
	"context"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
)

type fakeEvaluator struct{ result approvalmodel.PolicyResult }

func (e fakeEvaluator) Evaluate(context.Context, approvalmodel.Request) (approvalmodel.PolicyResult, error) {
	return e.result, nil
}

func TestEngineDelegatesToEvaluator(t *testing.T) {
	engine, err := NewEngine(fakeEvaluator{result: approvalmodel.PolicyResult{Type: approvalmodel.PolicyAllow, Reason: "ok"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Evaluate(context.Background(), approvalmodel.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != approvalmodel.PolicyAllow || result.Reason != "ok" {
		t.Fatalf("result = %+v, want delegated allow", result)
	}
}

func TestNewEngineRequiresEvaluator(t *testing.T) {
	if _, err := NewEngine(nil); err == nil {
		t.Fatal("NewEngine(nil) expected error")
	}
}
