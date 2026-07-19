package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFactoriesSetKind(t *testing.T) {
	if NewUserMessage("hi").Kind != MessageKindUserTurn {
		t.Fatal("user")
	}
	if NewSystemMessage("sys").Kind != MessageKindSystem {
		t.Fatal("system")
	}
	if NewToolResultMessage("id", "ok").Kind != MessageKindToolResult {
		t.Fatal("tool")
	}
	if NewSkillInjectionMessage("<skill_injection/>").Kind != MessageKindSkillInjection {
		t.Fatal("skill")
	}
	if NewReminderMessage("r").Kind != MessageKindReminder {
		t.Fatal("reminder")
	}
	if NewConversationSummaryMessage("s").Kind != MessageKindConversationSummary {
		t.Fatal("summary")
	}
}

func TestForUIHidesSkillInjection(t *testing.T) {
	msgs := []*Message{
		NewUserMessage("做一份 PPT"),
		NewSkillInjectionMessage("<skill_injection>\nbody\n</skill_injection>").WithSource(MessageSourceSkillGateway),
		{Role: RoleAssistant, Content: "已完成", Kind: MessageKindAssistant},
		NewToolResultMessage("t1", `{"ok":true}`),
	}
	ui := ForUI(msgs)
	if len(ui) != 2 {
		t.Fatalf("ui len=%d want 2: %+v", len(ui), ui)
	}
	if ui[0].Kind != MessageKindUserTurn || ui[1].Kind != MessageKindAssistant {
		t.Fatalf("ui kinds = %q %q", ui[0].Kind, ui[1].Kind)
	}
}

func TestForUIHidesEmptyToolCallAssistant(t *testing.T) {
	msgs := []*Message{
		NewUserMessage("hi"),
		{Role: RoleAssistant, Kind: MessageKindAssistant, ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "Read"}}}},
		NewToolResultMessage("c1", "ok"),
		NewAssistantMessage("最终回答"),
	}
	ui := ForUI(msgs)
	if len(ui) != 2 {
		t.Fatalf("ui len=%d want 2: %+v", len(ui), ui)
	}
	if ui[1].Content != "最终回答" {
		t.Fatalf("got=%+v", ui[1])
	}
}

func TestForModelKeepsSkillInjection(t *testing.T) {
	msgs := []*Message{
		NewUserMessage("hi"),
		NewSkillInjectionMessage("body"),
	}
	model := ForModel(msgs)
	if len(model) != 2 || model[1].Kind != MessageKindSkillInjection {
		t.Fatalf("model=%+v", model)
	}
}

func TestForModelConvertsLatestTaskListSnapshotToReminder(t *testing.T) {
	now := time.Now()
	oldList := TaskList{
		ID:        "list-1",
		SessionID: "sess-1",
		Title:     "旧清单",
		Items: []TaskListItem{
			{ID: "a", Text: "旧任务", Status: TaskListItemPending},
		},
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	latestList := TaskList{
		ID:        "list-1",
		SessionID: "sess-1",
		Title:     "新清单",
		Items: []TaskListItem{
			{ID: "a", Text: "已完成任务", Status: TaskListItemDone},
			{ID: "b", Text: "进行中任务", Status: TaskListItemDoing},
		},
		Version:   2,
		CreatedAt: now,
		UpdatedAt: now,
	}
	oldJSON, _ := json.Marshal(oldList)
	latestJSON, _ := json.Marshal(latestList)

	model := ForModel([]*Message{
		NewUserMessage("开始"),
		{Role: RoleAssistant, Content: string(oldJSON), Kind: MessageKindTaskListSnapshot},
		NewAssistantMessage("处理中"),
		{Role: RoleAssistant, Content: string(latestJSON), Kind: MessageKindTaskListSnapshot},
	})

	if len(model) != 2 {
		t.Fatalf("len=%d model=%+v", len(model), model)
	}
	if model[0].Content != "开始" || model[1].Content != "处理中" {
		t.Fatalf("unexpected model messages: %+v", model)
	}
	for _, msg := range model {
		if msg.Kind == MessageKindTaskListSnapshot {
			t.Fatalf("task_list_snapshot leaked to model: %+v", msg)
		}
	}
}

func TestEnsureKindFallback(t *testing.T) {
	m := &Message{Role: RoleUser, Content: "legacy"}
	m.EnsureKind()
	if m.Kind != MessageKindUserTurn {
		t.Fatalf("kind=%q", m.Kind)
	}
}

func TestSessionMessagesFromRunSkipsSystemAndHistory(t *testing.T) {
	history := []*Message{
		NewUserMessage("旧问题"),
		NewAssistantMessage("旧回答"),
	}
	all := []*Message{
		NewSystemMessage("本轮 system"),
		history[0],
		history[1],
		NewUserMessage("新问题"),
		NewSkillInjectionMessage("<skill_injection>body</skill_injection>"),
		NewToolResultMessage("t1", `{"ok":true}`),
		NewAssistantMessage("新回答"),
	}
	got := SessionMessagesFromRun(all, len(history))
	if len(got) != 4 {
		t.Fatalf("len=%d got=%+v", len(got), got)
	}
	if got[0].Kind != MessageKindUserTurn || got[1].Kind != MessageKindSkillInjection {
		t.Fatalf("got[0]=%+v got[1]=%+v", got[0], got[1])
	}
	if got[2].Kind != MessageKindToolResult || got[3].Kind != MessageKindAssistant {
		t.Fatalf("got[2]=%+v got[3]=%+v", got[2], got[3])
	}
}

func TestSessionMessagesFromRunEmpty(t *testing.T) {
	if SessionMessagesFromRun(nil, 0) != nil {
		t.Fatal("nil")
	}
	all := []*Message{NewSystemMessage("sys")}
	if SessionMessagesFromRun(all, 0) != nil {
		t.Fatal("only system")
	}
}
