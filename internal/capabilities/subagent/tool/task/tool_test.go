package task

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	submodel "genesis-agent/internal/capabilities/subagent/model"
	"genesis-agent/internal/capabilities/subagent/service"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/multiagent/contextsnapshot"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

type fakeController struct {
	request  contract.SpawnRequest
	instance model.Instance
	resumed  string
}

func (c *fakeController) Spawn(_ context.Context, request contract.SpawnRequest) (model.Instance, error) {
	c.request = request
	return model.Instance{AgentID: "agent-1", Status: model.StatusRunning}, nil
}
func (c *fakeController) Wait(_ context.Context, _ string) (model.Instance, error) {
	return model.Instance{AgentID: "agent-1", Status: model.StatusCompleted, Summary: "done"}, nil
}
func (c *fakeController) Stop(context.Context, string) error { return nil }
func (c *fakeController) Get(context.Context, string) (model.Instance, error) {
	return c.instance, nil
}
func (c *fakeController) Resume(_ context.Context, agentID, _ string) (model.Instance, error) {
	c.resumed = agentID
	return model.Instance{AgentID: "agent-2", Status: model.StatusRunning}, nil
}

type approved struct{}

func (approved) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

type recordingBackgroundRunner struct {
	started chan string
}

func (r recordingBackgroundRunner) Run(_ context.Context, agentID string) error {
	r.started <- agentID
	return nil
}

func TestTaskCreatesReadOnlyChildWithoutOrchestrationTools(t *testing.T) {
	controller := &fakeController{}
	created, err := New(Deps{
		Catalog:      service.NewMemoryCatalog([]submodel.Definition{{Name: "explore", SystemPrompt: "explore", ReadOnly: true}}),
		Controller:   controller,
		BaseAgent:    &domain.Agent{ID: "root", Name: "root"},
		AllowedTools: []string{"read_file", "write_file", "Skill", "Task", "TaskOutput", "TaskStop"},
		Approval:     approved{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "parent")
	ctx = contextutil.WithSessionID(ctx, "session")
	if _, err := created.Execute(ctx, `{"subagent_type":"explore","prompt":"find usage"}`); err != nil {
		t.Fatal(err)
	}
	if len(controller.request.Agent.Tools) != 1 || controller.request.Agent.Tools[0].Name != "read_file" {
		t.Fatalf("unexpected child tools: %+v", controller.request.Agent.Tools)
	}
	sys := controller.request.Agent.SystemPrompt
	if !strings.Contains(controller.request.Prompt, "[委派任务]") ||
		!strings.Contains(sys, "独立子智能体") ||
		!strings.Contains(sys, "InheritedRuntimeContract") ||
		!strings.Contains(sys, "角色说明") {
		t.Fatalf("Task did not build the isolated child contract: %+v", controller.request)
	}
}

func TestTaskStartsBackgroundRunnerForAsyncLaunch(t *testing.T) {
	controller := &fakeController{}
	runner := recordingBackgroundRunner{started: make(chan string, 1)}
	created, err := New(Deps{
		Catalog:    service.NewMemoryCatalog([]submodel.Definition{{Name: "explore", SystemPrompt: "explore"}}),
		Controller: controller,
		BaseAgent:  &domain.Agent{ID: "root", Name: "root"},
		Approval:   approved{},
		Background: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.Execute(context.Background(), `{"subagent_type":"explore","prompt":"find usage","run_in_background":true}`); err != nil {
		t.Fatal(err)
	}
	select {
	case agentID := <-runner.started:
		if agentID != "agent-1" {
			t.Fatalf("unexpected background agent id: %q", agentID)
		}
	case <-time.After(time.Second):
		t.Fatal("background runner was not started")
	}
}

func TestTaskForkContextUsesOnlySanitizedWhitelist(t *testing.T) {
	controller := &fakeController{}
	created, err := New(Deps{
		Catalog:      service.NewMemoryCatalog([]submodel.Definition{{Name: "explore", SystemPrompt: "explore", ReadOnly: true}}),
		Controller:   controller,
		BaseAgent:    &domain.Agent{ID: "root", Name: "root"},
		AllowedTools: []string{"read_file"},
		Approval:     approved{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "parent")
	ctx = contextutil.WithSessionID(ctx, "session")
	ctx = contextutil.WithTenantID(ctx, "tenant")
	ctx = contextsnapshot.WithParentSnapshot(ctx, []*domain.Message{
		domain.NewSystemMessage("root-only instruction"),
		domain.NewUserMessage("api_key=secret-value"),
		{Role: domain.RoleAssistant, Content: "Task draft", Kind: domain.MessageKindAssistant, ToolCalls: []domain.ToolCall{{ID: "call-1"}}},
		domain.NewAssistantMessage("prior final answer"),
		domain.NewToolResultMessage("call-1", "tool secret"),
	}, "call-1")
	if _, err := created.Execute(ctx, `{"subagent_type":"explore","prompt":"continue work","fork_context":true}`); err != nil {
		t.Fatal(err)
	}
	prompt := controller.request.Prompt
	if !strings.Contains(prompt, "api_key=[redacted]") || !strings.Contains(prompt, "prior final answer") {
		t.Fatalf("expected whitelisted background, got %q", prompt)
	}
	if strings.Contains(prompt, "root-only") || strings.Contains(prompt, "Task draft") || strings.Contains(prompt, "tool secret") {
		t.Fatalf("unsafe parent context leaked: %q", prompt)
	}
}

func TestTaskForkContextRejectsMissingSnapshot(t *testing.T) {
	created, err := New(Deps{
		Catalog:    service.NewMemoryCatalog([]submodel.Definition{{Name: "explore", SystemPrompt: "explore"}}),
		Controller: &fakeController{},
		BaseAgent:  &domain.Agent{ID: "root", Name: "root"},
		Approval:   approved{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.Execute(context.Background(), `{"subagent_type":"explore","prompt":"continue work","fork_context":true}`); err == nil {
		t.Fatal("expected fork without trusted snapshot to fail")
	}
}

func TestTaskResumeRequiresSameParentOwnership(t *testing.T) {
	controller := &fakeController{instance: model.Instance{AgentID: "agent-1", ParentRunID: "parent", SessionID: "session", TenantID: "tenant", SubagentType: "explore", Result: &model.TaskResult{Summary: "safe prior result"}}}
	created, err := New(Deps{Catalog: service.NewMemoryCatalog(nil), Controller: controller, BaseAgent: &domain.Agent{ID: "root"}, Approval: approved{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "parent")
	ctx = contextutil.WithSessionID(ctx, "session")
	ctx = contextutil.WithTenantID(ctx, "tenant")
	if _, err := created.Execute(ctx, `{"resume":"agent-1","prompt":"继续验证"}`); err != nil {
		t.Fatal(err)
	}
	if controller.resumed != "agent-1" {
		t.Fatalf("resume was not forwarded: %q", controller.resumed)
	}
}

func TestTaskAllowedToolsCanOnlyFurtherRestrictChild(t *testing.T) {
	controller := &fakeController{}
	created, err := New(Deps{Catalog: service.NewMemoryCatalog([]submodel.Definition{{Name: "explore", SystemPrompt: "explore"}}), Controller: controller, BaseAgent: &domain.Agent{ID: "root"}, AllowedTools: []string{"read_file", "grep"}, Approval: approved{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.Execute(context.Background(), `{"subagent_type":"explore","prompt":"inspect","allowed_tools":["grep","write_file"]}`); err != nil {
		t.Fatal(err)
	}
	if len(controller.request.Agent.Tools) != 1 || controller.request.Agent.Tools[0].Name != "grep" {
		t.Fatalf("allowed tools expanded child permissions: %+v", controller.request.Agent.Tools)
	}
}

func TestTaskAllowsOneNestedDelegationWhenDefinitionSetsMaxDepthTwo(t *testing.T) {
	controller := &fakeController{}
	created, err := New(Deps{
		Catalog:      service.NewMemoryCatalog([]submodel.Definition{{Name: "coordinator", SystemPrompt: "coordinate", MaxDepth: 2}}),
		Controller:   controller,
		BaseAgent:    &domain.Agent{ID: "root", Name: "root"},
		AllowedTools: []string{"read_file", "Task", "TaskOutput", "TaskStop"},
		Approval:     approved{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.Execute(context.Background(), `{"subagent_type":"coordinator","prompt":"coordinate"}`); err != nil {
		t.Fatal(err)
	}
	if controller.request.Depth != 1 || controller.request.MaxDepth != 2 {
		t.Fatalf("unexpected depth contract: %+v", controller.request)
	}
	if len(controller.request.Agent.Tools) != 2 || controller.request.Agent.Tools[1].Name != "Task" {
		t.Fatalf("nested Task was not exposed to depth-1 child: %+v", controller.request.Agent.Tools)
	}
}

func TestTaskRejectsThirdLevelDelegation(t *testing.T) {
	created, err := New(Deps{
		Catalog:    service.NewMemoryCatalog([]submodel.Definition{{Name: "worker", SystemPrompt: "work", MaxDepth: 2}}),
		Controller: &fakeController{},
		BaseAgent:  &domain.Agent{ID: "root", Name: "root"},
		Approval:   approved{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := contract.WithDelegationDepth(context.Background(), 2)
	ctx = contract.WithMaxDelegationDepth(ctx, 2)
	if _, err := created.Execute(ctx, `{"subagent_type":"worker","prompt":"work"}`); err == nil {
		t.Fatal("expected third-level delegation to fail")
	}
}

func TestTaskNestedDelegationCannotExpandParentToolsOrReadOnly(t *testing.T) {
	controller := &fakeController{}
	created, err := New(Deps{
		Catalog:      service.NewMemoryCatalog([]submodel.Definition{{Name: "writer", SystemPrompt: "write", Tools: []string{"read_file", "write_file"}}}),
		Controller:   controller,
		BaseAgent:    &domain.Agent{ID: "root", Name: "root"},
		AllowedTools: []string{"read_file", "write_file", "Task"},
		Approval:     approved{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := contract.WithDelegationDepth(context.Background(), 1)
	ctx = contract.WithMaxDelegationDepth(ctx, 2)
	ctx = contract.WithDelegationReadOnly(ctx, true)
	ctx = contract.WithDelegationTools(ctx, []string{"read_file", "Task"})
	if _, err := created.Execute(ctx, `{"subagent_type":"writer","prompt":"try to write"}`); err != nil {
		t.Fatal(err)
	}
	if len(controller.request.Agent.Tools) != 1 || controller.request.Agent.Tools[0].Name != "read_file" || !controller.request.ReadOnly {
		t.Fatalf("nested delegation expanded inherited permissions: %+v", controller.request)
	}
}

func TestTaskAppliesDefinitionRuntimeDefaults(t *testing.T) {
	controller := &fakeController{}
	created, err := New(Deps{
		Catalog: service.NewMemoryCatalog([]submodel.Definition{{
			Name:          "researcher",
			SystemPrompt:  "research",
			ExecutionMode: submodel.ExecutionModeAsync,
			TimeoutSec:    30,
			MaxTokens:     100,
			MaxToolCalls:  4,
		}}),
		Controller: controller,
		BaseAgent:  &domain.Agent{ID: "root", Name: "root", RuntimePolicy: domain.RuntimePolicy{MaxTokens: 200, MaxToolCalls: 8}},
		Approval:   approved{},
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := created.Execute(context.Background(), `{"subagent_type":"researcher","prompt":"research"}`)
	if err != nil {
		t.Fatal(err)
	}
	var launched model.TaskLaunch
	if err := json.Unmarshal([]byte(output), &launched); err != nil || launched.Status != "async_launched" {
		t.Fatalf("definition async default was not applied: output=%q err=%v", output, err)
	}
	if controller.request.Agent.RuntimePolicy.MaxTokens != 100 || controller.request.Agent.RuntimePolicy.MaxToolCalls != 4 || controller.request.Timeout != 30*time.Second {
		t.Fatalf("runtime defaults were not applied: %+v", controller.request)
	}
}

func TestTaskResumeHonorsAsyncDefinitionDefault(t *testing.T) {
	controller := &fakeController{instance: model.Instance{AgentID: "agent-1", ParentRunID: "parent", SessionID: "session", TenantID: "tenant", SubagentType: "researcher", Result: &model.TaskResult{Summary: "safe"}}}
	created, err := New(Deps{
		Catalog:    service.NewMemoryCatalog([]submodel.Definition{{Name: "researcher", SystemPrompt: "research", ExecutionMode: submodel.ExecutionModeAsync}}),
		Controller: controller,
		BaseAgent:  &domain.Agent{ID: "root", Name: "root"},
		Approval:   approved{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithRunID(context.Background(), "parent")
	ctx = contextutil.WithSessionID(ctx, "session")
	ctx = contextutil.WithTenantID(ctx, "tenant")
	output, err := created.Execute(ctx, `{"resume":"agent-1","prompt":"continue"}`)
	if err != nil {
		t.Fatal(err)
	}
	var launched model.TaskLaunch
	if err := json.Unmarshal([]byte(output), &launched); err != nil || launched.Status != "async_launched" {
		t.Fatalf("resume ignored async default: output=%q err=%v", output, err)
	}
}

func TestTaskRejectsNegativeRuntimeLimits(t *testing.T) {
	created, err := New(Deps{
		Catalog:    service.NewMemoryCatalog([]submodel.Definition{{Name: "researcher", SystemPrompt: "research"}}),
		Controller: &fakeController{},
		BaseAgent:  &domain.Agent{ID: "root", Name: "root"},
		Approval:   approved{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.Execute(context.Background(), `{"subagent_type":"researcher","prompt":"research","timeout_seconds":-1}`); err == nil {
		t.Fatal("expected negative timeout to fail")
	}
}

var _ contract.BackgroundRunner = (*recordingBackgroundRunner)(nil)
