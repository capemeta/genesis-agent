// Package lifecycle 提供后台子智能体的固定工具 TaskOutput 与 TaskStop。
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"

	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

type output struct {
	AgentID string       `json:"agent_id"`
	Status  model.Status `json:"status"`
	Summary string       `json:"summary,omitempty"`
	Error   string       `json:"error,omitempty"`
}
type query struct {
	AgentID string `json:"agent_id"`
}

type OutputTool struct{ controller contract.Controller }
type StopTool struct{ controller contract.Controller }

func New(controller contract.Controller) (*OutputTool, *StopTool, error) {
	if controller == nil {
		return nil, nil, fmt.Errorf("subagent Controller不能为空")
	}
	return &OutputTool{controller: controller}, &StopTool{controller: controller}, nil
}

func (t *OutputTool) GetInfo() *tool.Info {
	return info("TaskOutput", "获取后台子智能体的当前或最终结果。")
}
func (t *StopTool) GetInfo() *tool.Info { return info("TaskStop", "取消后台子智能体。") }
func info(name, description string) *tool.Info {
	return &tool.Info{Name: name, Description: description, Parameters: &tool.ParameterSchema{Type: "object", Properties: map[string]*tool.ParameterSchema{"agent_id": {Type: "string", Description: "Task 返回的 agent_id"}}, Required: []string{"agent_id"}}, Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: name == "TaskOutput", ConcurrencySafe: true, NeedsPermission: true}}
}
func (t *OutputTool) Execute(ctx context.Context, params string) (string, error) {
	var in query
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析 TaskOutput 参数失败: %w", err)
	}
	instance, err := t.controller.Get(ctx, in.AgentID)
	if err != nil {
		return "", err
	}
	return marshal(instance)
}
func (t *StopTool) Execute(ctx context.Context, params string) (string, error) {
	var in query
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析 TaskStop 参数失败: %w", err)
	}
	if err := t.controller.Stop(ctx, in.AgentID); err != nil {
		return "", err
	}
	instance, err := t.controller.Get(ctx, in.AgentID)
	if err != nil {
		return "", err
	}
	return marshal(instance)
}
func marshal(instance model.Instance) (string, error) {
	raw, err := json.Marshal(output{AgentID: instance.AgentID, Status: instance.Status, Summary: instance.Summary, Error: instance.Error})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
