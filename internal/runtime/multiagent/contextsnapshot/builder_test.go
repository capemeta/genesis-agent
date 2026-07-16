package contextsnapshot

import (
	"errors"
	"strings"
	"testing"

	"genesis-agent/internal/domain"
)

type failingSanitizer struct{}

func (failingSanitizer) Sanitize(string) (string, error) {
	return "", errors.New("redaction unavailable")
}

func TestBuilderFiltersParentMessagesAndRedactsContent(t *testing.T) {
	messages := []*domain.Message{
		domain.NewSystemMessage("root-only instruction"),
		domain.NewUserMessage("api_key=secret-value"),
		{Role: domain.RoleAssistant, Content: "tool draft", Kind: domain.MessageKindAssistant, ToolCalls: []domain.ToolCall{{ID: "call-1"}}},
		{Role: domain.RoleAssistant, Content: "final context", Kind: domain.MessageKindAssistant},
		domain.NewToolResultMessage("call-1", "secret tool output"),
	}
	out, err := Builder{}.Build(Input{Mode: ModeFilteredHistory, Messages: messages, MaxRunes: 1000, Delegation: DelegationEnvelope{Objective: "inspect"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Snapshot) != 2 {
		t.Fatalf("expected two allowed messages, got %+v", out.Snapshot)
	}
	if strings.Contains(out.UserInput, "root-only") || strings.Contains(out.UserInput, "tool draft") || strings.Contains(out.UserInput, "secret tool output") {
		t.Fatalf("unsafe parent content leaked into input: %q", out.UserInput)
	}
	if !strings.Contains(out.UserInput, "api_key=[redacted]") || !strings.Contains(out.UserInput, "final context") {
		t.Fatalf("expected sanitized context, got %q", out.UserInput)
	}
}

func TestBuilderFailsClosedWhenSanitizerFails(t *testing.T) {
	_, err := (Builder{Sanitizer: failingSanitizer{}}).Build(Input{
		Mode:       ModeFilteredHistory,
		Messages:   []*domain.Message{domain.NewUserMessage("sensitive")},
		Delegation: DelegationEnvelope{Objective: "inspect"},
	})
	if err == nil || !strings.Contains(err.Error(), "脱敏失败") {
		t.Fatalf("expected fail-closed redaction error, got %v", err)
	}
}

func TestBuilderRejectsMandatoryContractOverBudget(t *testing.T) {
	_, err := Builder{}.Build(Input{Mode: ModeIsolated, MaxRunes: 4, RuntimeContract: "contract", Delegation: DelegationEnvelope{Objective: "inspect"}})
	if err == nil || !strings.Contains(err.Error(), "mandatory contract") {
		t.Fatalf("expected budget error, got %v", err)
	}
}
