package task

import (
	"context"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	submodel "genesis-agent/internal/capabilities/subagent/model"
	"genesis-agent/internal/capabilities/subagent/service"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

type fakeController struct{ request contract.SpawnRequest }

func (c *fakeController) Spawn(_ context.Context, request contract.SpawnRequest) (model.Instance, error) {
	c.request = request
	return model.Instance{AgentID: "agent-1", Status: model.StatusRunning}, nil
}
func (c *fakeController) Wait(_ context.Context, _ string) (model.Instance, error) {
	return model.Instance{AgentID: "agent-1", Status: model.StatusCompleted, Summary: "done"}, nil
}
func (c *fakeController) Stop(context.Context, string) error { return nil }
func (c *fakeController) Get(context.Context, string) (model.Instance, error) {
	return model.Instance{}, nil
}

type approved struct{}

func (approved) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
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
}
