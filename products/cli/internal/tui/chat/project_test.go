package chat

import (
	"testing"

	"genesis-agent/internal/domain"
)

func TestProjectUIMessagesHidesSkillInjection(t *testing.T) {
	msgs := []*domain.Message{
		domain.NewUserMessage("做个 PPT"),
		domain.NewSkillInjectionMessage("<skill_injection>body</skill_injection>"),
		domain.NewToolResultMessage("t1", "ok"),
		domain.NewAssistantMessage("已完成"),
	}
	got := projectUIMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[0].role != "user" || got[0].content != "做个 PPT" {
		t.Fatalf("user=%+v", got[0])
	}
	if got[1].role != "assistant" || got[1].content != "已完成" {
		t.Fatalf("assistant=%+v", got[1])
	}
}

func TestProjectUIMessagesShowsSummaryAsSystem(t *testing.T) {
	msgs := []*domain.Message{
		domain.NewConversationSummaryMessage("<conversation-summary>先前讨论了 A</conversation-summary>"),
		domain.NewUserMessage("继续"),
	}
	got := projectUIMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].role != "system" {
		t.Fatalf("summary role=%q", got[0].role)
	}
}
