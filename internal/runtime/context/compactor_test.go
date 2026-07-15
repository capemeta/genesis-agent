package context

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	"genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime"
)

// MockSessionStore 用于模拟会话 CAS 状态切换
type MockSessionStore struct {
	status map[string]domain.SessionState
}

type blockingHookDispatcher struct{ events []hookmodel.EventName }

func (d *blockingHookDispatcher) Dispatch(_ context.Context, event hookmodel.Event) (hookmodel.AggregateResult, error) {
	d.events = append(d.events, event.Name)
	return hookmodel.AggregateResult{Blocked: true, BlockReason: "test block"}, nil
}

func TestCompactorPreCompactHookBlocksBeforeMicroMutation(t *testing.T) {
	tmpDir := t.TempDir()
	c := NewDefaultCompactor(NewHeuristicEstimator(), &MockSessionStore{}, &MockShortTermMemory{}, tmpDir, nil, 1000, 3, 300, 0.85, 0.75, 10)
	rc := runtime.NewRunContext(&domain.Run{ID: "run", SessionID: "session"}, &domain.Agent{DefaultModel: "test"})
	rc.Messages = []*domain.Message{
		{Role: domain.RoleTool, ToolCallID: "old", Content: "very long old tool result", Kind: domain.MessageKindToolResult},
		{Role: domain.RoleTool, ToolCallID: "recent-1", Content: "recent tool result", Kind: domain.MessageKindToolResult},
		{Role: domain.RoleTool, ToolCallID: "recent-2", Content: "recent tool result", Kind: domain.MessageKindToolResult},
		{Role: domain.RoleTool, ToolCallID: "recent-3", Content: "recent tool result", Kind: domain.MessageKindToolResult},
	}
	dispatcher := &blockingHookDispatcher{}
	ctx := hookcontract.WithDispatcher(context.Background(), dispatcher)
	result, err := c.MaybeMicroCompact(ctx, rc)
	if err != nil || result.Triggered {
		t.Fatalf("expected blocked compaction without error, result=%+v err=%v", result, err)
	}
	if len(dispatcher.events) != 1 || dispatcher.events[0] != hookmodel.EventPreCompact {
		t.Fatalf("unexpected events: %+v", dispatcher.events)
	}
	if strings.Contains(rc.Messages[0].Content, "persisted-output") {
		t.Fatal("Hook must run before message mutation")
	}
}

func (m *MockSessionStore) CreateSession(ctx context.Context, session *domain.Session) error {
	return nil
}
func (m *MockSessionStore) GetSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	return &domain.Session{ID: sessionID, Status: m.status[sessionID]}, nil
}
func (m *MockSessionStore) ListSessions(ctx context.Context, query memory.SessionQuery) ([]*domain.Session, error) {
	return nil, nil
}
func (m *MockSessionStore) FindLatestSession(ctx context.Context, query memory.SessionQuery) (*domain.Session, error) {
	return nil, memory.ErrSessionNotFound
}
func (m *MockSessionStore) UpdateSession(ctx context.Context, session *domain.Session) error {
	return nil
}
func (m *MockSessionStore) UpdateStatus(ctx context.Context, sessionID string, expected, target domain.SessionState) (bool, error) {
	if m.status == nil {
		m.status = make(map[string]domain.SessionState)
	}
	current := m.status[sessionID]
	if current == "" {
		current = domain.SessionStateActive
	}
	if current != expected {
		return false, nil
	}
	m.status[sessionID] = target
	return true, nil
}
func (m *MockSessionStore) DeleteSession(ctx context.Context, sessionID string) error {
	return nil
}

// MockShortTermMemory 模拟 ShortTermMemory 的摘要功能
type MockShortTermMemory struct {
	summary *domain.SessionSummary
}

type extractorSpy struct {
	ref      memory.SessionRef
	messages []*domain.Message
}

func (s *extractorSpy) Submit(ref memory.SessionRef, msgs []*domain.Message) {
	s.ref = ref
	s.messages = append([]*domain.Message(nil), msgs...)
}

func (m *MockShortTermMemory) Append(ctx context.Context, ref memory.SessionRef, msgs []*domain.Message) error {
	return nil
}
func (m *MockShortTermMemory) GetRecent(ctx context.Context, ref memory.SessionRef, opt memory.RecentOptions) (memory.RecentResult, error) {
	return memory.RecentResult{}, nil
}
func (m *MockShortTermMemory) Summarize(ctx context.Context, ref memory.SessionRef, opt memory.SummarizeOptions) (memory.SummaryResult, error) {
	leafID := "msg-leaf-test-id"
	userTurns := 0
	for i := len(opt.Messages) - 1; i >= 0; i-- {
		if opt.Messages[i].NormalizedKind() != domain.MessageKindUserTurn {
			continue
		}
		userTurns++
		if userTurns > opt.KeepRecentTurns {
			leafID = opt.Messages[i].UUID
			break
		}
	}
	m.summary = &domain.SessionSummary{
		SessionID:   ref.SessionID,
		Content:     "## Technical Context\nSummarized technical details.",
		LeafID:      leafID,
		TokensCount: 20,
		Iteration:   1,
		CreatedAt:   time.Now(),
	}
	return memory.SummaryResult{
		Summary:     m.summary,
		TokensSaved: 1000,
	}, nil
}
func (m *MockShortTermMemory) GetSummary(ctx context.Context, ref memory.SessionRef) (*domain.SessionSummary, error) {
	return m.summary, nil
}
func (m *MockShortTermMemory) Clear(ctx context.Context, ref memory.SessionRef) error {
	return nil
}

func TestCompactor_MaybeMicroCompact(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "genesis-compactor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	estimator := NewHeuristicEstimator()
	store := &MockSessionStore{}
	mem := &MockShortTermMemory{}

	// 创建 DefaultCompactor：超 10 字符即为大工具结果，保留最近 3 次
	c := NewDefaultCompactor(
		estimator,
		store,
		mem,
		tmpDir,
		nil,
		1000, // context window
		3,    // keep turns
		300,  // keep budget
		0.85, // compact ratio
		0.75, // warn ratio
		10,   // toolResultMaxTokens
	)

	agent := &domain.Agent{
		Name:         "test-agent",
		DefaultModel: "gpt-4o",
	}
	run := &domain.Run{
		ID:        "run-1",
		SessionID: "session-1",
	}
	rc := runtime.NewRunContext(run, agent)

	// 构造 4 个不同的 tool_result。根据 keepLast = 3，第 4 个 "call-old-2" 会被判定为旧消息并被卸载
	msgs := []*domain.Message{
		domain.NewSystemMessage("system prompt"),
		domain.NewUserMessage("user text 1"),
		{
			Role:       domain.RoleTool,
			ToolCallID: "call-old-2",
			Content:    "very long old tool result 2", // 超过 10 字符
			Kind:       domain.MessageKindToolResult,
		},
		{
			Role:       domain.RoleTool,
			ToolCallID: "call-old-1",
			Content:    "very long old tool result 1", // 超过 10 字符
			Kind:       domain.MessageKindToolResult,
		},
		{
			Role:       domain.RoleTool,
			ToolCallID: "call-recent-1",
			Content:    "recent tool result 1", // 超过 10 字符
			Kind:       domain.MessageKindToolResult,
		},
		{
			Role:       domain.RoleTool,
			ToolCallID: "call-recent-2",
			Content:    "recent tool result 2", // 超过 10 字符
			Kind:       domain.MessageKindToolResult,
		},
		domain.NewUserMessage("user text 2"),
	}
	rc.Messages = msgs

	// 触发 L1 Micro-Compact
	res, err := c.MaybeMicroCompact(context.Background(), rc)
	if err != nil {
		t.Fatalf("MaybeMicroCompact failed: %v", err)
	}

	if !res.Triggered {
		t.Errorf("expected L1 compaction to trigger")
	}

	if res.Kind != CompactionKindMicro {
		t.Errorf("expected compaction kind to be micro, got %v", res.Kind)
	}

	// 验证："call-old-2" 应该被就地卸载为 <persisted-output...>
	if !strings.Contains(rc.Messages[2].Content, "<persisted-output") {
		t.Errorf("expected call-old-2 tool result to be microcompacted, got %s", rc.Messages[2].Content)
	}

	// 验证：最近的三个 tool_result 应该保留正文不被卸载
	if strings.Contains(rc.Messages[4].Content, "<persisted-output") {
		t.Errorf("expected recent tool result 1 to be kept intact")
	}
	if strings.Contains(rc.Messages[5].Content, "<persisted-output") {
		t.Errorf("expected recent tool result 2 to be kept intact")
	}

	// 验证文件真的存盘了
	logFile := filepath.Join(tmpDir, "session-1", "artifacts", "tool-call-old-2.log")
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Errorf("expected tool-call-old-2.log file to exist under artifacts dir")
	}
}

func TestCompactor_MaybeAutoCompact(t *testing.T) {
	estimator := NewHeuristicEstimator()
	store := &MockSessionStore{}
	mem := &MockShortTermMemory{}

	// 创建 DefaultCompactor：窗口 20 tokens，超 80% (16) 触发
	c := NewDefaultCompactor(
		estimator,
		store,
		mem,
		"",
		nil,
		20,
		2,    // keep turns
		50,   // keep budget
		0.80, // compact ratio
		0.70, // warn ratio
		100,  // toolResultMaxTokens
	)

	agent := &domain.Agent{
		Name:         "test-agent",
		DefaultModel: "gpt-4o",
	}
	run := &domain.Run{
		ID:        "run-1",
		SessionID: "session-1",
	}
	rc := runtime.NewRunContext(run, agent)

	// 构造超长消息文本，很容易超过 20 tokens 的极低上限
	longText := strings.Repeat("hello world and coding agent test ", 10)
	rc.Messages = []*domain.Message{
		domain.NewSystemMessage("system prompt"),
		domain.NewUserMessage(longText),
		domain.NewUserMessage(longText),
		domain.NewUserMessage(longText),
		domain.NewUserMessage(longText),
		domain.NewUserMessage(longText),
	}

	ref := memory.SessionRef{SessionID: "session-1"}

	// 触发 L2 Auto-Compact
	res, err := c.MaybeAutoCompact(context.Background(), rc, ref)
	if err != nil {
		t.Fatalf("MaybeAutoCompact failed: %v", err)
	}

	if !res.Triggered {
		t.Errorf("expected L2 compaction to trigger")
	}

	if res.Kind != CompactionKindAuto {
		t.Errorf("expected compaction kind to be auto, got %v", res.Kind)
	}

	// 验证：rc.Messages 应该被重构成包含 system、summary 以及最新保留的两个 user_turn
	if len(rc.Messages) != 4 {
		t.Errorf("expected 4 messages after auto compact, got %d", len(rc.Messages))
	}

	if rc.Messages[1].NormalizedKind() != domain.MessageKindConversationSummary {
		t.Errorf("expected second message to be conversation summary, got %v", rc.Messages[1].NormalizedKind())
	}
}

func TestCompactor_MaybeAutoCompactExtractsOnlyCompactedHistory(t *testing.T) {
	estimator := NewHeuristicEstimator()
	store := &MockSessionStore{}
	mem := &MockShortTermMemory{}
	spy := &extractorSpy{}
	compactor := NewDefaultCompactor(estimator, store, mem, "", spy, 20, 2, 0, 0.8, 0.7, 100)
	rc := runtime.NewRunContext(&domain.Run{ID: "run-1", SessionID: "session-1"}, &domain.Agent{DefaultModel: "gpt-4o"})
	longText := strings.Repeat("compact me ", 20)
	rc.Messages = []*domain.Message{
		domain.NewSystemMessage("system"),
		domain.NewUserMessage(longText),
		domain.NewUserMessage(longText),
		domain.NewUserMessage(longText),
		domain.NewUserMessage(longText),
		domain.NewUserMessage(longText),
	}
	if _, err := compactor.MaybeAutoCompact(context.Background(), rc, memory.SessionRef{SessionID: "session-1", TenantID: "tenant-1"}); err != nil {
		t.Fatalf("MaybeAutoCompact: %v", err)
	}
	if len(spy.messages) != 3 {
		t.Fatalf("extractor received %d messages, want only 3 compacted messages", len(spy.messages))
	}
	if spy.ref.TenantID != "tenant-1" {
		t.Fatalf("extractor SessionRef lost tenant: %#v", spy.ref)
	}
}
