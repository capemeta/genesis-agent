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
		name     string
		in       string
		want     model.DecisionType
		scope    model.GrantScope
		pathMode model.PathGrantMode
	}{
		{name: "allow once", in: "y\n", want: model.DecisionApproved, scope: model.GrantScopeOnce},
		{name: "allow session file", in: "s\n", want: model.DecisionApprovedForScope, scope: model.GrantScopeSession, pathMode: model.PathGrantExact},
		{name: "allow session dir", in: "d\n", want: model.DecisionApprovedForScope, scope: model.GrantScopeSession, pathMode: model.PathGrantDirectory},
		{name: "allow project file", in: "p\n", want: model.DecisionApprovedForScope, scope: model.GrantScopeProject, pathMode: model.PathGrantExact},
		{name: "allow project dir", in: "f\n", want: model.DecisionApprovedForScope, scope: model.GrantScopeProject, pathMode: model.PathGrantDirectory},
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
			if got.Type != tt.want || got.Scope != tt.scope || got.PathMode != tt.pathMode {
				t.Fatalf("decision = (%s, %s, %s), want (%s, %s, %s)", got.Type, got.Scope, got.PathMode, tt.want, tt.scope, tt.pathMode)
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
		ToolName: "read_file",
		Action:   model.ActionFileRead,
		Resource: model.Resource{
			Type:     "file",
			URI:      "file:///tmp/dir/a.txt",
			Display:  "/tmp/dir/a.txt",
			Metadata: map[string]string{"backend": `/tmp/dir/a.txt`},
		},
		Reason:   "workspace 外路径需要确认",
		Risk:     model.RiskHigh,
		Metadata: map[string]string{"backend": `/tmp/dir/a.txt`},
		SuggestedScopes: []model.GrantScope{
			model.GrantScopeOnce,
			model.GrantScopeSession,
			model.GrantScopeProject,
		},
	}
}

func samplePolicy() model.PolicyResult {
	return model.PolicyResult{
		Type:   model.PolicyAsk,
		Reason: "workspace 外路径需要确认",
		Risk:   model.RiskHigh,
		SuggestedScopes: []model.GrantScope{
			model.GrantScopeOnce,
			model.GrantScopeSession,
			model.GrantScopeProject,
		},
	}
}
