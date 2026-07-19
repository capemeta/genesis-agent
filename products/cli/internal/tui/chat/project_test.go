package chat

import (
	"encoding/json"
	"testing"
	"time"

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

func TestLoadPlanFromMessagesUsesLatestSnapshot(t *testing.T) {
	now := time.Now()
	oldPlan := domain.TaskList{
		ID:        "plan-1",
		SessionID: "sess-1",
		Title:     "旧计划",
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	latestPlan := domain.TaskList{
		ID:        "plan-1",
		SessionID: "sess-1",
		Title:     "最新计划",
		Version:   2,
		Items: []domain.TaskListItem{
			{ID: "a", Text: "继续执行", Status: domain.TaskListItemDoing},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	oldJSON, _ := json.Marshal(oldPlan)
	latestJSON, _ := json.Marshal(latestPlan)

	got := loadPlanFromMessages([]*domain.Message{
		domain.NewUserMessage("开始"),
		{Role: domain.RoleAssistant, Content: string(oldJSON), Kind: domain.MessageKindTaskListSnapshot},
		domain.NewAssistantMessage("中间回答"),
		{Role: domain.RoleAssistant, Content: string(latestJSON), Kind: domain.MessageKindTaskListSnapshot},
	})

	if got == nil {
		t.Fatal("expected plan")
	}
	if got.Title != "最新计划" || got.Version != 2 {
		t.Fatalf("got=%+v", got)
	}
	if len(got.Items) != 1 || got.Items[0].Status != domain.TaskListItemDoing {
		t.Fatalf("items=%+v", got.Items)
	}
}
