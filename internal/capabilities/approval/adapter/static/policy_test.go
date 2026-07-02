package static

import (
	"context"
	"testing"

	"genesis-agent/internal/capabilities/approval/model"
)

func TestPolicyAllowsWorkspaceWrite(t *testing.T) {
	result, err := NewPolicyEngine().Evaluate(context.Background(), requestWithMetadata(map[string]string{"scope": "workspace"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != model.PolicyAllow {
		t.Fatalf("Policy = %q, want allow", result.Type)
	}
}

func TestPolicyAsksForExternalResource(t *testing.T) {
	result, err := NewPolicyEngine().Evaluate(context.Background(), requestWithMetadata(map[string]string{"scope": "external"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != model.PolicyAsk {
		t.Fatalf("Policy = %q, want ask", result.Type)
	}
}

func TestPolicyDeniesProtectedResource(t *testing.T) {
	result, err := NewPolicyEngine().Evaluate(context.Background(), requestWithMetadata(map[string]string{"scope": "protected"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != model.PolicyDeny {
		t.Fatalf("Policy = %q, want deny", result.Type)
	}
}

func TestPolicyAsksForDangerousResource(t *testing.T) {
	result, err := NewPolicyEngine().Evaluate(context.Background(), requestWithMetadata(map[string]string{"dangerous": "true"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != model.PolicyAsk {
		t.Fatalf("Policy = %q, want ask", result.Type)
	}
}

func TestPolicyDeniesCriticalResource(t *testing.T) {
	result, err := NewPolicyEngine().Evaluate(context.Background(), requestWithMetadata(map[string]string{"critical": "true"}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != model.PolicyDeny {
		t.Fatalf("Policy = %q, want deny", result.Type)
	}
}

func requestWithMetadata(metadata map[string]string) model.Request {
	return model.Request{
		Action: model.ActionFileWrite,
		Resource: model.Resource{
			Type:     "file",
			URI:      "workspace://a.txt",
			Metadata: metadata,
		},
		Metadata: metadata,
	}
}

func TestPolicyAsksForUnclassifiedResource(t *testing.T) {
	result, err := NewPolicyEngine().Evaluate(context.Background(), model.Request{
		Action:   model.ActionCommandExec,
		Resource: model.Resource{Type: "command", URI: "command://powershell"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != model.PolicyAsk {
		t.Fatalf("Policy = %q, want ask", result.Type)
	}
}

func TestPolicyDangerousOnlySuggestsOnce(t *testing.T) {
	result, err := NewPolicyEngine().Evaluate(context.Background(), requestWithMetadata(map[string]string{"dangerous": "true"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SuggestedScopes) != 1 || result.SuggestedScopes[0] != model.GrantScopeOnce {
		t.Fatalf("SuggestedScopes = %#v, want once only", result.SuggestedScopes)
	}
}

func TestPolicyExternalSuggestsSession(t *testing.T) {
	result, err := NewPolicyEngine().Evaluate(context.Background(), requestWithMetadata(map[string]string{"scope": "external"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SuggestedScopes) != 2 || result.SuggestedScopes[1] != model.GrantScopeSession {
		t.Fatalf("SuggestedScopes = %#v, want once and session", result.SuggestedScopes)
	}
}
