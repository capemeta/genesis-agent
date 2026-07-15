package react

import (
	"context"
	"testing"

	contract "genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
)

type memStore struct {
	msgs []*domain.Message
}

func (m *memStore) Append(_ context.Context, _ contract.SessionRef, messages []*domain.Message) error {
	m.msgs = append(m.msgs, messages...)
	return nil
}

func (m *memStore) GetRecent(_ context.Context, _ contract.SessionRef, _ contract.RecentOptions) (contract.RecentResult, error) {
	return contract.RecentResult{
		Messages:  append([]*domain.Message(nil), m.msgs...),
		Truncated: false,
	}, nil
}

func (m *memStore) Clear(_ context.Context, _ contract.SessionRef) error {
	m.msgs = nil
	return nil
}

func (m *memStore) Summarize(_ context.Context, _ contract.SessionRef, _ contract.SummarizeOptions) (contract.SummaryResult, error) {
	return contract.SummaryResult{}, nil
}

func (m *memStore) GetSummary(_ context.Context, _ contract.SessionRef) (*domain.SessionSummary, error) {
	return nil, nil
}

func TestPersistRunSessionMessagesSavesFullChain(t *testing.T) {
	store := &memStore{}
	e := &ReactLoopEngine{memory: store}
	rc := runtime.NewRunContext(&domain.Run{ID: "r1"}, &domain.Agent{})
	history := []*domain.Message{
		domain.NewUserMessage("旧问"),
		domain.NewAssistantMessage("旧答"),
	}
	rc.Messages = append(rc.Messages, domain.NewSystemMessage("sys"))
	rc.Messages = append(rc.Messages, history...)
	rc.Messages = append(rc.Messages,
		domain.NewUserMessage("新问"),
		domain.NewSkillInjectionMessage("<skill_injection>x</skill_injection>"),
		domain.NewToolResultMessage("c1", `{"type":"skill_loaded"}`),
		domain.NewAssistantMessage("新答"),
	)

	e.persistRunSessionMessages(context.Background(), "s1", rc, len(history), logger.NewNop())

	if len(store.msgs) != 4 {
		t.Fatalf("saved=%d %+v", len(store.msgs), store.msgs)
	}
	kinds := []domain.MessageKind{
		store.msgs[0].Kind, store.msgs[1].Kind, store.msgs[2].Kind, store.msgs[3].Kind,
	}
	want := []domain.MessageKind{
		domain.MessageKindUserTurn,
		domain.MessageKindSkillInjection,
		domain.MessageKindToolResult,
		domain.MessageKindAssistant,
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds=%v want %v", kinds, want)
		}
	}
	ui := domain.ForUI(store.msgs)
	if len(ui) != 2 || ui[0].Content != "新问" || ui[1].Content != "新答" {
		t.Fatalf("ForUI=%+v", ui)
	}
}
