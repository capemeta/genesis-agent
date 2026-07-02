package approval

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"genesis-agent/internal/capabilities/approval/model"
)

func TestTerminalRequesterDecisions(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  model.DecisionType
		scope model.GrantScope
	}{
		{name: "allow once", in: "o\n", want: model.DecisionApproved, scope: model.GrantScopeOnce},
		{name: "allow session", in: "s\n", want: model.DecisionApprovedForScope, scope: model.GrantScopeSession},
		{name: "deny", in: "n\n", want: model.DecisionDenied, scope: model.GrantScopeOnce},
		{name: "abort", in: "a\n", want: model.DecisionAbort, scope: model.GrantScopeOnce},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			r := NewTerminalRequester(strings.NewReader(tt.in), &out)

			got, err := r.RequestApproval(context.Background(), sampleRequest(), samplePolicy())
			if err != nil {
				t.Fatalf("RequestApproval() error = %v", err)
			}
			if got.Type != tt.want || got.Scope != tt.scope {
				t.Fatalf("decision = (%s, %s), want (%s, %s)", got.Type, got.Scope, tt.want, tt.scope)
			}
		})
	}
}

func TestTerminalRequesterRetriesInvalidInput(t *testing.T) {
	var out bytes.Buffer
	r := NewTerminalRequester(strings.NewReader("bad\no\n"), &out)

	got, err := r.RequestApproval(context.Background(), sampleRequest(), samplePolicy())
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if got.Type != model.DecisionApproved {
		t.Fatalf("decision = %s, want %s", got.Type, model.DecisionApproved)
	}
	if !strings.Contains(out.String(), "输入无效") {
		t.Fatalf("output should contain invalid input hint, got %q", out.String())
	}
}

func TestTerminalRequesterDoesNotAllowSessionWhenScopeNotSuggested(t *testing.T) {
	var out bytes.Buffer
	r := NewTerminalRequester(strings.NewReader("s\no\n"), &out)
	policy := samplePolicy()
	policy.SuggestedScopes = []model.GrantScope{model.GrantScopeOnce}

	got, err := r.RequestApproval(context.Background(), sampleRequest(), policy)
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if got.Type != model.DecisionApproved || got.Scope != model.GrantScopeOnce {
		t.Fatalf("decision = (%s, %s), want approved once", got.Type, got.Scope)
	}
	output := out.String()
	if strings.Contains(output, "允许本会话") {
		t.Fatalf("output should not advertise session scope, got %q", output)
	}
}

func TestTerminalRequesterDeniesWhenNotInitialized(t *testing.T) {
	var out bytes.Buffer
	r := NewTerminalRequester(nil, &out)

	got, err := r.RequestApproval(context.Background(), sampleRequest(), samplePolicy())
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if got.Type != model.DecisionDenied {
		t.Fatalf("decision = %s, want %s", got.Type, model.DecisionDenied)
	}
}
func TestTerminalRequesterDeniesOnEmptyEOF(t *testing.T) {
	var out bytes.Buffer
	r := NewTerminalRequester(strings.NewReader(""), &out)

	got, err := r.RequestApproval(context.Background(), sampleRequest(), samplePolicy())
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if got.Type != model.DecisionDenied {
		t.Fatalf("decision = %s, want %s", got.Type, model.DecisionDenied)
	}
}

func sampleRequest() model.Request {
	return model.Request{
		ToolName:        "read_file",
		Action:          model.ActionFileRead,
		Resource:        model.Resource{Type: "file", URI: "file:///tmp/a.txt", Display: "/tmp/a.txt"},
		Reason:          "workspace 外路径需要确认",
		Risk:            model.RiskHigh,
		SuggestedScopes: []model.GrantScope{model.GrantScopeOnce, model.GrantScopeSession},
	}
}

func samplePolicy() model.PolicyResult {
	return model.PolicyResult{
		Type:            model.PolicyAsk,
		Reason:          "workspace 外路径需要确认",
		Risk:            model.RiskHigh,
		SuggestedScopes: []model.GrantScope{model.GrantScopeOnce, model.GrantScopeSession},
	}
}
