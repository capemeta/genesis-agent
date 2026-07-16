package task

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	llmcontract "genesis-agent/internal/capabilities/llm/contract"
	memoryinmemory "genesis-agent/internal/capabilities/memory/adapter/inmemory"
	submodel "genesis-agent/internal/capabilities/subagent/model"
	"genesis-agent/internal/capabilities/subagent/service"
	subagentlifecycle "genesis-agent/internal/capabilities/subagent/tool/lifecycle"
	toolregistry "genesis-agent/internal/capabilities/tool/adapter/registry"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	traceadapter "genesis-agent/internal/capabilities/trace/adapter"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/logger"
	runtimecontext "genesis-agent/internal/runtime/context"
	multibackground "genesis-agent/internal/runtime/multiagent/background"
	"genesis-agent/internal/runtime/multiagent/controller"
	"genesis-agent/internal/runtime/prompt"
	"genesis-agent/internal/runtime/strategy/react"
)

type scriptedLLM struct {
	mu    sync.Mutex
	calls int
}

func (m *scriptedLLM) GetModelName() string { return "scripted" }

func (m *scriptedLLM) Generate(ctx context.Context, messages []*domain.Message, tools []*toolcontract.Info) (*domain.Message, error) {
	return m.StreamGenerate(ctx, messages, tools, func(string, bool) {})
}

func (m *scriptedLLM) StreamGenerate(_ context.Context, messages []*domain.Message, _ []*toolcontract.Info, _ func(string, bool)) (*domain.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	switch m.calls {
	case 1:
		return &domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "task-1", Type: "function", Function: domain.FunctionCall{Name: "Task", Arguments: `{"subagent_type":"explore","prompt":"检查配置并给出结论","fork_context":true}`}}}}, nil
	case 2:
		for _, message := range messages {
			if message != nil && strings.Contains(message.Content, "parent-only instruction") {
				return nil, context.Canceled
			}
		}
		if len(messages) == 0 || !strings.Contains(messages[len(messages)-1].Content, "[背景 user]\n请委派检查") {
			return nil, context.Canceled
		}
		return domain.NewAssistantMessage("子任务结论 token=child-secret"), nil
	case 3:
		for _, message := range messages {
			if message != nil && message.Role == domain.RoleTool && strings.Contains(message.Content, `"result_id"`) && !strings.Contains(message.Content, "child-secret") {
				return domain.NewAssistantMessage("已收到子任务的安全摘要。"), nil
			}
		}
		return nil, context.Canceled
	default:
		return nil, context.Canceled
	}
}

var _ llmcontract.ChatModel = (*scriptedLLM)(nil)

type backgroundScriptedLLM struct{}

func (m *backgroundScriptedLLM) GetModelName() string { return "scripted-background" }

func (m *backgroundScriptedLLM) Generate(ctx context.Context, messages []*domain.Message, tools []*toolcontract.Info) (*domain.Message, error) {
	return m.StreamGenerate(ctx, messages, tools, func(string, bool) {})
}

func (m *backgroundScriptedLLM) StreamGenerate(_ context.Context, messages []*domain.Message, tools []*toolcontract.Info, _ func(string, bool)) (*domain.Message, error) {
	joined := joinMessageContent(messages)
	if strings.Contains(joined, "[委派任务]\n后台检查配置") {
		return domain.NewAssistantMessage("后台子任务安全摘要"), nil
	}
	if !hasTool(tools, "Task") {
		return domain.NewAssistantMessage("后台子任务安全摘要"), nil
	}
	if !strings.Contains(joined, "async_launched") {
		return &domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "task-bg-1", Type: "function", Function: domain.FunctionCall{Name: "Task", Arguments: `{"subagent_type":"explore","prompt":"后台检查配置","run_in_background":true}`}}}}, nil
	}
	agentID := extractAgentID(joined)
	if agentID == "" {
		return nil, fmt.Errorf("missing background agent id in parent messages: %s", joined)
	}
	if !strings.Contains(joined, `"result_delivered":true`) {
		return &domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "task-output-1", Type: "function", Function: domain.FunctionCall{Name: "TaskOutput", Arguments: fmt.Sprintf(`{"agent_id":%q,"block":true,"timeout_seconds":2}`, agentID)}}}}, nil
	}
	if !strings.Contains(joined, `"duplicate_result":true`) {
		return &domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "task-output-2", Type: "function", Function: domain.FunctionCall{Name: "TaskOutput", Arguments: fmt.Sprintf(`{"agent_id":%q,"block":true,"timeout_seconds":1}`, agentID)}}}}, nil
	}
	if strings.Count(joined, "后台子任务安全摘要") != 1 {
		return nil, fmt.Errorf("background result summary was injected more than once: %s", joined)
	}
	return domain.NewAssistantMessage("后台结果只消费一次。"), nil
}

func hasTool(tools []*toolcontract.Info, name string) bool {
	for _, info := range tools {
		if info != nil && info.Name == name {
			return true
		}
	}
	return false
}

func joinMessageContent(messages []*domain.Message) string {
	var b strings.Builder
	for _, message := range messages {
		if message == nil {
			continue
		}
		b.WriteString(message.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

var agentIDPattern = regexp.MustCompile(`"agent_id"\s*:\s*"([^"]+)"`)

func extractAgentID(text string) string {
	match := agentIDPattern.FindStringSubmatch(text)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

var _ llmcontract.ChatModel = (*backgroundScriptedLLM)(nil)

type integrationApproval struct{}

func (integrationApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

func TestReactParentTaskReceivesSafeChildResult(t *testing.T) {
	registry := toolregistry.NewRegistry()
	llm := &scriptedLLM{}
	engine := react.NewReactLoopEngine(
		llm,
		registry,
		memoryinmemory.NewInMemoryStore(),
		prompt.New(),
		logger.NewNop(),
		traceadapter.NewNopTracer(),
		runtimecontext.NewHeuristicEstimator(),
		runtimecontext.NewContextBudgetPlanner(nil),
		8_192,
		1_024,
	)
	limiter, err := controller.NewMemorySlotLimiter(2)
	if err != nil {
		t.Fatal(err)
	}
	childController, err := controller.New(engine, limiter, logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	parent := &domain.Agent{ID: "parent", Name: "parent", SystemPrompt: "parent-only instruction", Tools: []domain.ToolRef{{Name: "Task"}}, RuntimePolicy: domain.RuntimePolicy{MaxIterations: 4}}
	taskTool, err := New(Deps{
		Catalog:      service.NewMemoryCatalog([]submodel.Definition{{Name: "explore", SystemPrompt: "只返回最终结论"}}),
		Controller:   childController,
		BaseAgent:    parent,
		AllowedTools: nil,
		Approval:     integrationApproval{},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.Register(taskTool)

	run, err := engine.Start(context.Background(), domain.StartRunRequest{SessionID: "parent-session", TenantID: "tenant-a", UserInput: "请委派检查", Agent: parent})
	if err != nil {
		t.Fatal(err)
	}
	if run.FinalAnswer != "已收到子任务的安全摘要。" {
		t.Fatalf("unexpected parent answer: %q", run.FinalAnswer)
	}
	if llm.calls != 3 {
		t.Fatalf("expected parent -> child -> parent flow, calls=%d", llm.calls)
	}
}

func TestReactParentTaskOutputConsumesBackgroundResultOnce(t *testing.T) {
	registry := toolregistry.NewRegistry()
	llm := &backgroundScriptedLLM{}
	engine := react.NewReactLoopEngine(
		llm,
		registry,
		memoryinmemory.NewInMemoryStore(),
		prompt.New(),
		logger.NewNop(),
		traceadapter.NewNopTracer(),
		runtimecontext.NewHeuristicEstimator(),
		runtimecontext.NewContextBudgetPlanner(nil),
		8_192,
		1_024,
	)
	limiter, err := controller.NewMemorySlotLimiter(2)
	if err != nil {
		t.Fatal(err)
	}
	childController, err := controller.New(engine, limiter, logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	controlStore := multibackground.NewMemoryControlStore()
	backgroundRunner, err := multibackground.New(multibackground.Deps{
		Controller: childController,
		Leases:     controlStore,
		Heartbeats: controlStore,
		Cancels:    controlStore,
		OwnerID:    "test-worker",
	})
	if err != nil {
		t.Fatal(err)
	}
	parent := &domain.Agent{ID: "parent", Name: "parent", Tools: []domain.ToolRef{{Name: "Task"}, {Name: "TaskOutput"}}, RuntimePolicy: domain.RuntimePolicy{MaxIterations: 6}}
	taskTool, err := New(Deps{
		Catalog:    service.NewMemoryCatalog([]submodel.Definition{{Name: "explore", SystemPrompt: "只返回最终结论"}}),
		Controller: childController,
		BaseAgent:  parent,
		Approval:   integrationApproval{},
		Background: backgroundRunner,
	})
	if err != nil {
		t.Fatal(err)
	}
	outputTool, _, err := subagentlifecycle.New(childController)
	if err != nil {
		t.Fatal(err)
	}
	registry.Register(taskTool)
	registry.Register(outputTool)

	run, err := engine.Start(context.Background(), domain.StartRunRequest{SessionID: "parent-session", TenantID: "tenant-a", UserInput: "请后台委派检查，然后读取两次结果", Agent: parent})
	if err != nil {
		t.Fatal(err)
	}
	if run.FinalAnswer != "后台结果只消费一次。" {
		t.Fatalf("unexpected parent answer: %q", run.FinalAnswer)
	}
}
