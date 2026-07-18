package context

import (
	"context"
	"testing"

	"genesis-agent/internal/domain"
)

func TestAssembleHistorySkipsDuplicateSummary(t *testing.T) {
	a := NewDefaultContextAssembler(NewHeuristicEstimator())
	opt := AssemblerOptions{
		Budget:         DistributableBudget{History: 8000, Summary: 2000},
		Model:          "test",
		MaxInputTokens: 16000,
		SystemPrompt:   "sys",
		HistorySummary: &domain.SessionSummary{Content: "摘要A", TokensCount: 10},
		HistoryMessages: []*domain.Message{
			domain.NewConversationSummaryMessage("摘要A"),
			domain.NewUserMessage("旧问"),
			domain.NewAssistantMessage("旧答"),
		},
		CurrentUserMessage: domain.NewUserMessage("新问"),
	}

	msgs, err := a.Assemble(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}

	summaryCount := 0
	for _, m := range msgs {
		if m != nil && m.NormalizedKind() == domain.MessageKindConversationSummary {
			summaryCount++
		}
	}
	if summaryCount != 1 {
		t.Fatalf("summary injected %d times, want 1", summaryCount)
	}
}

func TestAssembleHistoryInjectsSummaryWhenMissing(t *testing.T) {
	a := NewDefaultContextAssembler(NewHeuristicEstimator())
	opt := AssemblerOptions{
		Budget:         DistributableBudget{History: 8000, Summary: 2000},
		Model:          "test",
		MaxInputTokens: 16000,
		SystemPrompt:   "sys",
		HistorySummary: &domain.SessionSummary{Content: "摘要B", TokensCount: 10},
		HistoryMessages: []*domain.Message{
			domain.NewUserMessage("旧问"),
			domain.NewAssistantMessage("旧答"),
		},
		CurrentUserMessage: domain.NewUserMessage("新问"),
	}

	msgs, err := a.Assemble(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}

	summaryCount := 0
	for _, m := range msgs {
		if m != nil && m.NormalizedKind() == domain.MessageKindConversationSummary {
			summaryCount++
			if m.Content != "摘要B" {
				t.Fatalf("summary content=%q", m.Content)
			}
		}
	}
	if summaryCount != 1 {
		t.Fatalf("summary injected %d times, want 1", summaryCount)
	}
}
