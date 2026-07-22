package react

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	contract "genesis-agent/internal/capabilities/memory/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
	traceadapter "genesis-agent/internal/capabilities/trace/adapter"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/logger"
	runtimecontext "genesis-agent/internal/runtime/context"
	"genesis-agent/internal/runtime/prompt"
)

// TestPersistOnLLMFailureMidRun 验证：长 Run 中途 LLM 失败时，已产生的增量消息链必须落盘，
// 以便同会话下一轮「请继续」能从短期记忆恢复任务上下文。
func TestPersistOnLLMFailureMidRun(t *testing.T) {
	store := &memStore{}
	llm := &scriptedChatModel{
		responses: []*domain.Message{
			{
				Role: domain.RoleAssistant,
				Kind: domain.MessageKindAssistant,
				ToolCalls: []domain.ToolCall{{
					ID:       "c1",
					Type:     "function",
					Function: domain.FunctionCall{Name: "ping", Arguments: `{}`},
				}},
			},
		},
		errs: []error{
			nil,
			fmt.Errorf("llm timeout"),
		},
	}
	e := NewReactLoopEngine(
		llm,
		&pingRegistry{},
		store,
		stubPromptBuilder{},
		logger.NewNop(),
		traceadapter.NewNopTracer(),
		runtimecontext.NewHeuristicEstimator(),
		runtimecontext.NewContextBudgetPlanner(nil),
		32000,
		4096,
	)

	_, err := e.Start(context.Background(), domain.StartRunRequest{
		RunID:     "run-fail-persist",
		SessionID: "sess-fail-persist",
		TenantID:  "dev",
		UserInput: "复制一份PPT并改价格",
		Agent: &domain.Agent{
			Name:         "Genesis Agent",
			DefaultModel: "test-model",
			RuntimePolicy: domain.RuntimePolicy{
				MaxIterations: 10,
			},
		},
	})
	if err == nil {
		t.Fatal("expected LLM failure")
	}
	if !strings.Contains(err.Error(), "LLM调用失败") && !strings.Contains(err.Error(), "llm timeout") {
		t.Fatalf("unexpected err: %v", err)
	}

	if len(store.msgs) == 0 {
		t.Fatal("failed run must persist session messages, got empty store")
	}

	var sawUser, sawToolResult, sawFailureMarker bool
	for _, m := range store.msgs {
		if m == nil {
			continue
		}
		switch m.NormalizedKind() {
		case domain.MessageKindUserTurn:
			if strings.Contains(m.Content, "复制一份PPT") {
				sawUser = true
			}
		case domain.MessageKindToolResult:
			sawToolResult = true
		case domain.MessageKindReminder:
			if strings.Contains(m.Content, "运行中断") || strings.Contains(m.Content, "llm timeout") {
				sawFailureMarker = true
			}
		}
	}
	if !sawUser {
		t.Fatalf("missing user_turn in persisted chain: %+v", kindsOf(store.msgs))
	}
	if !sawToolResult {
		t.Fatalf("missing tool_result in persisted chain: %+v", kindsOf(store.msgs))
	}
	if !sawFailureMarker {
		t.Fatalf("missing failure reminder in persisted chain: %+v", kindsOf(store.msgs))
	}

	// 同会话再次 Run：「请继续」必须能加载到上一轮失败前的历史。
	llm2 := &scriptedChatModel{
		responses: []*domain.Message{
			domain.NewAssistantMessage("继续完成PPT修改"),
		},
	}
	e.llm = llm2
	_, err = e.Start(context.Background(), domain.StartRunRequest{
		RunID:     "run-continue",
		SessionID: "sess-fail-persist",
		TenantID:  "dev",
		UserInput: "请继续",
		Agent: &domain.Agent{
			Name:          "Genesis Agent",
			DefaultModel:  "test-model",
			RuntimePolicy: domain.RuntimePolicy{MaxIterations: 5},
		},
	})
	if err != nil {
		t.Fatalf("continue run: %v", err)
	}
	if llm2.lastHistoryLen < 3 {
		t.Fatalf("continue run history too short: got %d msgs in model input, want prior failed-run context", llm2.lastHistoryLen)
	}
	joined := messagesContent(llm2.lastMessages)
	if !strings.Contains(joined, "复制一份PPT") {
		t.Fatalf("continue run model input missing prior user task, got: %s", truncateForTest(joined, 400))
	}
}

func TestPersistOnMaxIterations(t *testing.T) {
	store := &memStore{}
	llm := &scriptedChatModel{
		responses: []*domain.Message{
			{
				Role: domain.RoleAssistant,
				Kind: domain.MessageKindAssistant,
				ToolCalls: []domain.ToolCall{{
					ID:       "c1",
					Type:     "function",
					Function: domain.FunctionCall{Name: "ping", Arguments: `{}`},
				}},
			},
			{
				Role: domain.RoleAssistant,
				Kind: domain.MessageKindAssistant,
				ToolCalls: []domain.ToolCall{{
					ID:       "c2",
					Type:     "function",
					Function: domain.FunctionCall{Name: "ping", Arguments: `{}`},
				}},
			},
		},
	}
	e := NewReactLoopEngine(
		llm,
		&pingRegistry{},
		store,
		stubPromptBuilder{},
		logger.NewNop(),
		traceadapter.NewNopTracer(),
		runtimecontext.NewHeuristicEstimator(),
		runtimecontext.NewContextBudgetPlanner(nil),
		32000,
		4096,
	)

	_, err := e.Start(context.Background(), domain.StartRunRequest{
		RunID:     "run-max-iter",
		SessionID: "sess-max-iter",
		TenantID:  "dev",
		UserInput: "反复调用工具",
		Agent: &domain.Agent{
			Name:          "Genesis Agent",
			DefaultModel:  "test-model",
			RuntimePolicy: domain.RuntimePolicy{MaxIterations: 2},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "超过最大迭代次数") {
		t.Fatalf("expected max iteration error, got %v", err)
	}
	if len(store.msgs) == 0 {
		t.Fatal("max-iteration exit must persist session messages")
	}
	if !hasKind(store.msgs, domain.MessageKindUserTurn) || !hasKind(store.msgs, domain.MessageKindToolResult) {
		t.Fatalf("persisted chain incomplete: %+v", kindsOf(store.msgs))
	}
}

func TestGetRecentFailureAbortsRun(t *testing.T) {
	store := &failingGetRecentStore{err: errors.New("disk broken")}
	e := NewReactLoopEngine(
		&scriptedChatModel{responses: []*domain.Message{domain.NewAssistantMessage("should not run")}},
		&pingRegistry{},
		store,
		stubPromptBuilder{},
		logger.NewNop(),
		traceadapter.NewNopTracer(),
		runtimecontext.NewHeuristicEstimator(),
		runtimecontext.NewContextBudgetPlanner(nil),
		32000,
		4096,
	)

	_, err := e.Start(context.Background(), domain.StartRunRequest{
		RunID:     "run-getrecent-fail",
		SessionID: "sess-getrecent-fail",
		TenantID:  "dev",
		UserInput: "hello",
		Agent: &domain.Agent{
			Name:          "Genesis Agent",
			DefaultModel:  "test-model",
			RuntimePolicy: domain.RuntimePolicy{MaxIterations: 3},
		},
	})
	if err == nil {
		t.Fatal("expected GetRecent failure to abort run")
	}
	if !strings.Contains(err.Error(), "短期记忆") && !strings.Contains(err.Error(), "disk broken") {
		t.Fatalf("unexpected err: %v", err)
	}
	if store.appendCalls != 0 {
		t.Fatalf("must not append when history load failed, appendCalls=%d", store.appendCalls)
	}
}

type stubPromptBuilder struct{}

func (stubPromptBuilder) BuildSystem(context.Context, prompt.BuildRequest) (string, error) {
	return "test system prompt", nil
}

type scriptedChatModel struct {
	responses      []*domain.Message
	errs           []error
	calls          int
	lastMessages   []*domain.Message
	lastHistoryLen int
}

func (s *scriptedChatModel) Generate(ctx context.Context, messages []*domain.Message, tools []*tool.Info) (*domain.Message, error) {
	return s.StreamGenerate(ctx, messages, tools, nil)
}

func (s *scriptedChatModel) StreamGenerate(_ context.Context, messages []*domain.Message, _ []*tool.Info, _ func(string, bool)) (*domain.Message, error) {
	s.lastMessages = append([]*domain.Message(nil), messages...)
	s.lastHistoryLen = len(messages)
	i := s.calls
	s.calls++
	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	if i >= len(s.responses) || s.responses[i] == nil {
		return nil, fmt.Errorf("scriptedChatModel: no response for call %d", i)
	}
	return s.responses[i], nil
}

func (s *scriptedChatModel) GetModelName() string { return "scripted" }

type pingRegistry struct{}

func (pingRegistry) Register(tool.Tool) error { return nil }
func (pingRegistry) Replace(string, string, tool.Tool) error {
	return errors.New("unsupported")
}
func (pingRegistry) Owner(string) (string, bool) { return "", false }
func (pingRegistry) Unregister(string)           {}
func (pingRegistry) Get(string) tool.Tool        { return nil }
func (pingRegistry) Execute(context.Context, string, string) (string, error) {
	return `{"ok":true}`, nil
}
func (pingRegistry) ListInfos() []*tool.Info {
	return []*tool.Info{{Name: "ping", Description: "ping tool"}}
}
func (pingRegistry) FilterInfos([]string) []*tool.Info { return nil }
func (pingRegistry) Names() []string                   { return []string{"ping"} }

type failingGetRecentStore struct {
	err         error
	appendCalls int
}

func (f *failingGetRecentStore) Append(_ context.Context, _ contract.SessionRef, _ []*domain.Message) error {
	f.appendCalls++
	return nil
}
func (f *failingGetRecentStore) GetRecent(context.Context, contract.SessionRef, contract.RecentOptions) (contract.RecentResult, error) {
	return contract.RecentResult{}, f.err
}
func (f *failingGetRecentStore) Clear(context.Context, contract.SessionRef) error { return nil }
func (f *failingGetRecentStore) Summarize(context.Context, contract.SessionRef, contract.SummarizeOptions) (contract.SummaryResult, error) {
	return contract.SummaryResult{}, nil
}
func (f *failingGetRecentStore) GetSummary(context.Context, contract.SessionRef) (*domain.SessionSummary, error) {
	return nil, nil
}

func kindsOf(msgs []*domain.Message) []domain.MessageKind {
	out := make([]domain.MessageKind, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		out = append(out, m.NormalizedKind())
	}
	return out
}

func hasKind(msgs []*domain.Message, kind domain.MessageKind) bool {
	for _, m := range msgs {
		if m != nil && m.NormalizedKind() == kind {
			return true
		}
	}
	return false
}

func messagesContent(msgs []*domain.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m == nil {
			continue
		}
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func truncateForTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
