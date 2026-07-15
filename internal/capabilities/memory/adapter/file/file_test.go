package file

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	contract "genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
	runtimecontext "genesis-agent/internal/runtime/context"
)

func TestFileShortTermMemorySessionStore(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	mem := NewFileShortTermMemory(tmpDir, runtimecontext.NewHeuristicEstimator(), nil)
	ctx := context.Background()

	older := &domain.Session{ID: "session-older", TenantID: "tenant-a", UserID: "user-a", AgentID: "agent-a"}
	if err := mem.CreateSession(ctx, older); err != nil {
		t.Fatalf("CreateSession older: %v", err)
	}
	newer := &domain.Session{ID: "session-newer", TenantID: "tenant-a", UserID: "user-a", AgentID: "agent-a"}
	if err := mem.CreateSession(ctx, newer); err != nil {
		t.Fatalf("CreateSession newer: %v", err)
	}

	got, err := mem.GetSession(ctx, newer.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.TenantID != newer.TenantID || got.UserID != newer.UserID || got.AgentID != newer.AgentID {
		t.Fatalf("session metadata was not persisted: %#v", got)
	}

	latest, err := mem.FindLatestSession(ctx, contract.SessionQuery{TenantID: "tenant-a", UserID: "user-a", AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("FindLatestSession: %v", err)
	}
	if latest.ID != newer.ID {
		t.Fatalf("latest session = %q, want %q", latest.ID, newer.ID)
	}

	if err := mem.DeleteSession(ctx, newer.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	sessions, err := mem.ListSessions(ctx, contract.SessionQuery{TenantID: "tenant-a", UserID: "user-a", AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != older.ID {
		t.Fatalf("deleted session should be excluded, got %#v", sessions)
	}
	if _, err := mem.GetSession(ctx, "../outside"); err == nil {
		t.Fatal("GetSession accepted a path-like session ID")
	}
	if err := mem.Append(ctx, contract.SessionRef{SessionID: "../outside"}, []*domain.Message{domain.NewUserMessage("x")}); err == nil {
		t.Fatal("Append accepted a path-like session ID")
	}
	if _, err := mem.GetSession(ctx, "missing"); !errors.Is(err, contract.ErrSessionNotFound) {
		t.Fatalf("missing session error = %v, want ErrSessionNotFound", err)
	}
}

func TestFileShortTermMemorySupportsLargeJSONLMessage(t *testing.T) {
	mem := NewFileShortTermMemory(t.TempDir(), runtimecontext.NewHeuristicEstimator(), nil)
	ref := contract.SessionRef{SessionID: "large-message-session"}
	content := strings.Repeat("x", 128*1024)
	if err := mem.Append(context.Background(), ref, []*domain.Message{domain.NewToolResultMessage("call-1", content)}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	result, err := mem.GetRecent(context.Background(), ref, contract.RecentOptions{})
	if err != nil {
		t.Fatalf("GetRecent: %v", err)
	}
	if len(result.Messages) != 1 || result.Messages[0].Content != content {
		t.Fatalf("large JSONL message was not restored")
	}
}

func TestFileShortTermMemoryForkAndReplay(t *testing.T) {
	mem := NewFileShortTermMemory(t.TempDir(), runtimecontext.NewHeuristicEstimator(), nil)
	ctx := context.Background()
	source := contract.SessionRef{SessionID: "source-session"}
	messages := []*domain.Message{domain.NewUserMessage("first"), domain.NewAssistantMessage("first answer"), domain.NewUserMessage("second")}
	if err := mem.Append(ctx, source, messages); err != nil {
		t.Fatalf("Append source: %v", err)
	}
	if err := mem.Fork(ctx, source, contract.SessionRef{SessionID: "fork-session"}, messages[1].UUID); err != nil {
		t.Fatalf("Fork: %v", err)
	}
	forked, err := mem.Replay(ctx, contract.SessionRef{SessionID: "fork-session"}, "")
	if err != nil {
		t.Fatalf("Replay fork: %v", err)
	}
	if len(forked) != 2 || forked[1].Content != "first answer" {
		contents := make([]string, 0, len(forked))
		for _, message := range forked {
			contents = append(contents, message.Content)
		}
		t.Fatalf("forked history contents = %#v", contents)
	}
	original, err := mem.Replay(ctx, source, "")
	if err != nil || len(original) != 3 {
		t.Fatalf("source was modified: messages=%#v err=%v", original, err)
	}
}

func TestFileShortTermMemory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "genesis-memory-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	estimator := runtimecontext.NewHeuristicEstimator()
	mem := NewFileShortTermMemory(tmpDir, estimator, nil)
	ctx := context.Background()
	ref := contract.SessionRef{SessionID: "test-session-1"}

	// 1. 测试写入与全部读回
	msgs := []*domain.Message{
		domain.NewSystemMessage("system persona"),
		domain.NewUserMessage("user text 1"),
	}

	if err := mem.Append(ctx, ref, msgs); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	res, err := mem.GetRecent(ctx, ref, contract.RecentOptions{})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}

	if len(res.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(res.Messages))
	}

	// 2. 测试 Clear 清空
	if err := mem.Clear(ctx, ref); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	res, err = mem.GetRecent(ctx, ref, contract.RecentOptions{})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(res.Messages) != 0 {
		t.Errorf("expected 0 messages after clear, got %d", len(res.Messages))
	}
}

func TestFileShortTermMemory_ToolChainProtection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "genesis-memory-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	estimator := runtimecontext.NewHeuristicEstimator()
	mem := NewFileShortTermMemory(tmpDir, estimator, nil)
	ctx := context.Background()
	ref := contract.SessionRef{SessionID: "test-session-tool"}

	// 构造一段含有 ToolChain 的历史
	msgs := []*domain.Message{
		domain.NewUserMessage("user turn 1"), // Index 0 (user_turn)
		{
			Role:    domain.RoleAssistant, // Index 1
			Content: "",
			ToolCalls: []domain.ToolCall{
				{
					ID:       "call-1",
					Type:     "function",
					Function: domain.FunctionCall{Name: "run_code", Arguments: "{}"},
				},
			},
			Kind: domain.MessageKindAssistant,
		},
		domain.NewToolResultMessage("call-1", "result output"), // Index 2
		domain.NewUserMessage("user turn 2"),                   // Index 3 (user_turn)
	}

	if err := mem.Append(ctx, ref, msgs); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// 设置 MaxTurns = 1
	// 期望：因为 Index 3 是 1 轮 user_turn；
	// 倒推遇到 Index 2 (tool_result) 属于 pending，不能在此处截断，必须强行往回吃，
	// 吃下 Index 1 (assistant tool_call) 闭合 call-1 之后，才可以触发截断判定。
	// 所以最终应该得到 Index 1、2、3 (共3条消息)，而不是仅有 Index 3。
	res, err := mem.GetRecent(ctx, ref, contract.RecentOptions{MaxTurns: 1})
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}

	if len(res.Messages) != 3 {
		t.Errorf("expected 3 messages due to ToolChain protection, got %d", len(res.Messages))
	}

	if res.Messages[0].Role != domain.RoleAssistant {
		t.Errorf("expected first message after truncation to be Assistant, got %v", res.Messages[0].Role)
	}
}
