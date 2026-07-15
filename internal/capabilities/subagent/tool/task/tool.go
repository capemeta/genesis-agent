// Package task 实现固定 SubAgent 网关工具 Task。
package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/capabilities/subagent/service"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

const toolName = "Task"

// Deps 是 Task 的产品无关依赖。
type Deps struct {
	Catalog      service.Catalog
	Controller   contract.Controller
	BaseAgent    *domain.Agent
	AllowedTools []string
	Approval     approvalcontract.Service
}

// Tool 是唯一的 Phase 1 子智能体 LLM 委派入口。
type Tool struct{ deps Deps }

type input struct {
	SubagentType string `json:"subagent_type"`
	Prompt       string `json:"prompt"`
	Description  string `json:"description,omitempty"`
	Background   bool   `json:"run_in_background,omitempty"`
	MaxTurns     int    `json:"max_turns,omitempty"`
	MaxTokens    int64  `json:"max_tokens,omitempty"`
	MaxToolCalls int    `json:"max_tool_calls,omitempty"`
	TimeoutSec   int    `json:"timeout_seconds,omitempty"`
}

type output struct {
	AgentID string       `json:"agent_id"`
	Status  model.Status `json:"status"`
	Summary string       `json:"summary,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// New 创建 Task。
func New(deps Deps) (*Tool, error) {
	if deps.Catalog == nil || deps.Controller == nil || deps.BaseAgent == nil || deps.Approval == nil {
		return nil, fmt.Errorf("Task 依赖不完整")
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{Name: toolName, Description: "委派独立子智能体执行任务。", DescriptionFunc: t.description, Parameters: &tool.ParameterSchema{Type: "object", Properties: map[string]*tool.ParameterSchema{"subagent_type": {Type: "string", Description: "来自 available_agents 的子智能体类型"}, "prompt": {Type: "string", Description: "给子智能体的完整、独立任务说明"}, "description": {Type: "string", Description: "委派摘要，用于审批和进度"}, "run_in_background": {Type: "boolean", Description: "为 true 时立即返回 agent_id"}, "max_turns": {Type: "integer", Description: "子 Run 最大轮次，仅可收紧"}, "max_tokens": {Type: "integer", Description: "子 Run token 硬预算，仅可收紧"}, "max_tool_calls": {Type: "integer", Description: "子 Run 工具调用硬上限，仅可收紧"}, "timeout_seconds": {Type: "integer", Description: "子 Run 墙钟超时秒数，仅可收紧"}}, Required: []string{"subagent_type", "prompt"}}, Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: true, NeedsPermission: true}}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析 Task 参数失败: %w", err)
	}
	definition, ok := t.deps.Catalog.Get(in.SubagentType)
	if !ok {
		return "", fmt.Errorf("未知 subagent_type %q，请从 available_agents 中选择", in.SubagentType)
	}
	parentRunID, _ := contextutil.GetRunID(ctx)
	sessionID, _ := contextutil.GetSessionID(ctx)
	tenantID, _ := contextutil.GetTenantID(ctx)
	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{ToolName: toolName, Action: approvalmodel.ActionSubAgentDelegate, Resource: approvalmodel.Resource{Type: "subagent", URI: definition.Name, Display: definition.Name}, Reason: firstNonEmpty(in.Description, "委派子智能体"), Risk: approvalmodel.RiskMedium, Metadata: map[string]string{"subagent_type": definition.Name}})
	if err != nil {
		return "", fmt.Errorf("Task 审批失败: %w", err)
	}
	if decision.Type != approvalmodel.DecisionApproved && decision.Type != approvalmodel.DecisionApprovedForScope {
		return "", fmt.Errorf("Task 未获授权: %s", firstNonEmpty(decision.Reason, string(decision.Type)))
	}
	agent := t.childAgent(definition.SystemPrompt, definition.ReadOnly, definition.Tools)
	if definition.MaxTurns > 0 {
		agent.RuntimePolicy.MaxIterations = stricterInt(agent.RuntimePolicy.MaxIterations, definition.MaxTurns)
	}
	if in.MaxTurns > 0 {
		agent.RuntimePolicy.MaxIterations = stricterInt(agent.RuntimePolicy.MaxIterations, in.MaxTurns)
	}
	if in.MaxTokens > 0 {
		agent.RuntimePolicy.MaxTokens = stricterInt64(agent.RuntimePolicy.MaxTokens, in.MaxTokens)
	}
	if in.MaxToolCalls > 0 {
		agent.RuntimePolicy.MaxToolCalls = stricterInt(agent.RuntimePolicy.MaxToolCalls, in.MaxToolCalls)
	}
	instance, err := t.deps.Controller.Spawn(ctx, contract.SpawnRequest{SessionID: sessionID, TenantID: tenantID, ParentRunID: parentRunID, SubagentType: definition.Name, Prompt: strings.TrimSpace(in.Prompt), Agent: agent, Timeout: time.Duration(in.TimeoutSec) * time.Second})
	if err != nil {
		return "", err
	}
	if in.Background {
		return encode(output{AgentID: instance.AgentID, Status: "async_launched"})
	}
	instance, err = t.deps.Controller.Wait(ctx, instance.AgentID)
	if err != nil {
		return "", err
	}
	return encode(output{AgentID: instance.AgentID, Status: instance.Status, Summary: instance.Summary, Error: instance.Error})
}

func encode(value output) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("编码 Task 结果失败: %w", err)
	}
	return string(encoded), nil
}

func stricterInt(current, requested int) int {
	if current == 0 || requested < current {
		return requested
	}
	return current
}
func stricterInt64(current, requested int64) int64 {
	if current == 0 || requested < current {
		return requested
	}
	return current
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (t *Tool) description(context.Context) (string, error) {
	return service.RenderDescription(t.deps.Catalog)
}

func (t *Tool) childAgent(systemPrompt string, readOnly bool, definitionTools []string) *domain.Agent {
	clone := *t.deps.BaseAgent
	clone.ID = "subagent-" + clone.ID
	clone.Name = "SubAgent"
	clone.SystemPrompt = systemPrompt
	clone.Tools = make([]domain.ToolRef, 0, len(t.deps.AllowedTools))
	allowed := t.deps.AllowedTools
	if len(definitionTools) > 0 {
		allowed = intersect(allowed, definitionTools)
	}
	for _, name := range allowed {
		if name == toolName || name == "TaskOutput" || name == "TaskStop" || name == "Skill" {
			continue
		}
		if readOnly && !isReadOnly(name) {
			continue
		}
		clone.Tools = append(clone.Tools, domain.ToolRef{Name: name})
	}
	return &clone
}

func intersect(parent, requested []string) []string {
	wanted := map[string]bool{}
	for _, name := range requested {
		wanted[name] = true
	}
	out := make([]string, 0, len(parent))
	for _, name := range parent {
		if wanted[name] {
			out = append(out, name)
		}
	}
	return out
}

func isReadOnly(name string) bool {
	switch name {
	case "current_time", "calculator", "read_file", "list_dir", "walk_dir", "glob", "grep", "web_search", "web_fetch", "list_mcp_resources", "read_mcp_resource":
		return true
	default:
		return false
	}
}
