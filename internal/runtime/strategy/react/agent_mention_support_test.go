package react

import (
	"context"
	"strings"
	"testing"

	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime"
	multicontract "genesis-agent/internal/runtime/multiagent/contract"
)

func TestInjectAgentMentions(t *testing.T) {
	e := &ReactLoopEngine{}
	rc := &runtime.RunContext{Messages: []*domain.Message{domain.NewUserMessage("请 @run-agent-explore 查一下")}}
	e.injectAgentMentions(context.Background(), rc, "请 @run-agent-explore 查一下，也可 @agent-plan")
	if len(rc.Messages) != 3 {
		t.Fatalf("want user + 2 reminders, got %d", len(rc.Messages))
	}
	for _, msg := range rc.Messages[1:] {
		if msg.Kind != domain.MessageKindReminder {
			t.Fatalf("want reminder kind, got %s", msg.Kind)
		}
		if !strings.Contains(msg.Content, "必须调用 Task") || !strings.Contains(msg.Content, "subagent_type=") {
			t.Fatalf("reminder missing Task gate: %s", msg.Content)
		}
	}
	if !strings.Contains(rc.Messages[1].Content, `"explore"`) {
		t.Fatalf("explore mention missing: %s", rc.Messages[1].Content)
	}
	if !strings.Contains(rc.Messages[2].Content, `"plan"`) {
		t.Fatalf("plan mention missing: %s", rc.Messages[2].Content)
	}
}

func TestInjectAgentMentionsEmpty(t *testing.T) {
	e := &ReactLoopEngine{}
	rc := &runtime.RunContext{Messages: []*domain.Message{domain.NewUserMessage("普通问题")}}
	e.injectAgentMentions(context.Background(), rc, "普通问题")
	if len(rc.Messages) != 1 {
		t.Fatalf("no agent mention should not append: %d", len(rc.Messages))
	}
}

func TestInjectAgentMentionsSkippedOnSubRun(t *testing.T) {
	e := &ReactLoopEngine{}
	rc := &runtime.RunContext{Messages: []*domain.Message{domain.NewUserMessage("请 @run-agent-explore")}}
	ctx := multicontract.WithDelegationDepth(context.Background(), 1)
	e.injectAgentMentions(ctx, rc, "请 @run-agent-explore")
	if len(rc.Messages) != 1 {
		t.Fatalf("sub run must not inject agent mention reminders: %d", len(rc.Messages))
	}
}
