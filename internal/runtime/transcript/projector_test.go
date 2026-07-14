package transcript

import (
	"testing"

	"genesis-agent/internal/domain"
)

func TestProjectForModelKeepsSkillAndTools(t *testing.T) {
	msgs := []*domain.Message{
		domain.NewSystemMessage("sys"),
		domain.NewUserMessage("hi"),
		domain.NewSkillInjectionMessage("skill body"),
		domain.NewToolResultMessage("t1", "ok"),
		domain.NewAssistantMessage("done"),
	}
	got := ProjectForModel(msgs)
	if len(got) != 5 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestProjectForUICLIHidesSkillAndTools(t *testing.T) {
	msgs := []*domain.Message{
		domain.NewUserMessage("hi"),
		domain.NewSkillInjectionMessage("skill body"),
		domain.NewToolResultMessage("t1", "ok"),
		domain.NewAssistantMessage("done"),
		domain.NewConversationSummaryMessage("<conversation-summary>sum</conversation-summary>"),
		domain.NewSystemMessage("<repeat_guard>x</repeat_guard>"),
	}
	got := ProjectForUI(msgs, DefaultCLIPolicy())
	if len(got) != 3 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[0].Kind != domain.MessageKindUserTurn || got[1].Kind != domain.MessageKindAssistant {
		t.Fatalf("got=%+v", got)
	}
	if got[2].Kind != domain.MessageKindConversationSummary {
		t.Fatalf("summary missing: %+v", got)
	}
}

func TestProjectForUIHidesEmptyToolCallAssistant(t *testing.T) {
	msgs := []*domain.Message{
		domain.NewUserMessage("hi"),
		{Role: domain.RoleAssistant, Kind: domain.MessageKindAssistant, ToolCalls: []domain.ToolCall{{ID: "c1"}}},
		domain.NewAssistantMessage("done"),
	}
	got := ProjectForUI(msgs, DefaultCLIPolicy())
	if len(got) != 2 || got[1].Content != "done" {
		t.Fatalf("got=%+v", got)
	}
}

func TestProjectForUICanHideSummary(t *testing.T) {
	policy := DefaultCLIPolicy()
	policy.ShowConversationSummary = false
	msgs := []*domain.Message{
		domain.NewUserMessage("hi"),
		domain.NewConversationSummaryMessage("sum"),
		domain.NewAssistantMessage("done"),
	}
	got := ProjectForUI(msgs, policy)
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
}
