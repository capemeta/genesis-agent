package llmlogger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	llmlogger "genesis-agent/internal/capabilities/llm/logger"
	"genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
)

type mockChatModel struct {
	modelName string
	response  *domain.Message
}

func (m *mockChatModel) Generate(ctx context.Context, messages []*domain.Message, tools []*tool.Info) (*domain.Message, error) {
	return m.response, nil
}

func (m *mockChatModel) StreamGenerate(ctx context.Context, messages []*domain.Message, tools []*tool.Info, onDelta func(delta string, isThought bool)) (*domain.Message, error) {
	if m.response != nil {
		if m.response.ReasoningContent != "" {
			onDelta(m.response.ReasoningContent, true)
		}
		if m.response.Content != "" {
			onDelta(m.response.Content, false)
		}
	}
	return m.response, nil
}

func (m *mockChatModel) GetModelName() string {
	return m.modelName
}

func TestLoggingChatModel_GenerateAndStreamGenerate(t *testing.T) {
	var buf bytes.Buffer
	inner := &mockChatModel{
		modelName: "test-model",
		response: &domain.Message{
			Role:             domain.RoleAssistant,
			Content:          "Hello back!",
			ReasoningContent: "Thinking step 1",
			Kind:             domain.MessageKindAssistant,
		},
	}

	params := map[string]any{
		"provider":    "openai",
		"temperature": 0.7,
	}

	wrapped := llmlogger.Wrap(inner, &buf, params)
	if wrapped.GetModelName() != "test-model" {
		t.Fatalf("unexpected model name: %s", wrapped.GetModelName())
	}

	msgs := []*domain.Message{
		domain.NewSystemMessage("System prompt"),
		domain.NewUserMessage("Hello LLM"),
	}
	tools := []*tool.Info{
		{
			Name:        "test_tool",
			Description: "A test tool",
		},
	}

	// 1. Test Generate with Context IDs
	ctx := contextutil.WithRunID(context.Background(), "run-test-123")
	ctx = contextutil.WithSessionID(ctx, "session-test-456")

	_, err := wrapped.Generate(ctx, msgs, tools)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// 2. Test StreamGenerate
	var deltaAcc string
	_, err = wrapped.StreamGenerate(ctx, msgs, tools, func(d string, isThought bool) {
		deltaAcc += d
	})
	if err != nil {
		t.Fatalf("StreamGenerate failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines in llm.log buffer, got %d", len(lines))
	}

	var rec1 llmlogger.LLMCallRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec1); err != nil {
		t.Fatalf("unmarshal rec1 failed: %v", err)
	}
	if rec1.CallType != "Generate" || rec1.Model != "test-model" {
		t.Fatalf("invalid rec1: %+v", rec1)
	}
	if rec1.RunID != "run-test-123" || rec1.SessionID != "session-test-456" {
		t.Fatalf("expected RunID=run-test-123 and SessionID=session-test-456, got RunID=%q, SessionID=%q", rec1.RunID, rec1.SessionID)
	}
	if len(rec1.Messages) != 2 || len(rec1.Tools) != 1 || rec1.Response == nil {
		t.Fatalf("incomplete payload in rec1: %+v", rec1)
	}
	if rec1.Response.ReasoningContent != "Thinking step 1" || rec1.Response.Content != "Hello back!" {
		t.Fatalf("response payload mismatch in rec1: %+v", rec1.Response)
	}

	var rec2 llmlogger.LLMCallRecord
	if err := json.Unmarshal([]byte(lines[1]), &rec2); err != nil {
		t.Fatalf("unmarshal rec2 failed: %v", err)
	}
	if rec2.CallType != "StreamGenerate" || rec2.Model != "test-model" {
		t.Fatalf("invalid rec2: %+v", rec2)
	}
}
