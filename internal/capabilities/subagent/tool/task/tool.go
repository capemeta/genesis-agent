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
	subagentcontract "genesis-agent/internal/capabilities/subagent/contract"
	subagentmodel "genesis-agent/internal/capabilities/subagent/model"
	subagentprompt "genesis-agent/internal/capabilities/subagent/prompt"
	"genesis-agent/internal/capabilities/subagent/service"
	tool "genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/multiagent/contextsnapshot"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
	"genesis-agent/internal/runtime/progress"
)

const toolName = "Task"
const maxDelegationInputRunes = 32_000

// InputResolver 是产品控制面的裸路径到已授权 ResourceRef 转换端口。
type InputResolver interface {
	ResolveAvailableInputs(ctx context.Context, inputs []string) ([]workmodel.ResourceRef, error)
}

// Deps 是 Task 的产品无关依赖。
type Deps struct {
	Catalog        service.Catalog
	Controller     contract.Controller
	BaseAgent      *domain.Agent
	AllowedTools   []string
	Approval       approvalcontract.Service
	SnapshotSource contextsnapshot.TranscriptSnapshotSource
	Background     contract.BackgroundRunner
	InputResolver  InputResolver
	// DelegationPosture 影响 Task Description 中的委派姿态文案。
	DelegationPosture string
	// MaxConcurrent 写入 Description 的并行提示（与 SlotLimiter 硬限一致）。
	MaxConcurrent int
}

// Tool 是唯一的 Phase 1 子智能体 LLM 委派入口。
type Tool struct {
	deps           Deps
	snapshotSource contextsnapshot.TranscriptSnapshotSource
}

type input struct {
	SubagentType string   `json:"subagent_type"`
	Prompt       string   `json:"prompt"`
	Description  string   `json:"description,omitempty"`
	Background   bool     `json:"run_in_background,omitempty"`
	MaxTurns     int      `json:"max_turns,omitempty"`
	MaxTokens    int64    `json:"max_tokens,omitempty"`
	MaxToolCalls int      `json:"max_tool_calls,omitempty"`
	TimeoutSec   int      `json:"timeout_seconds,omitempty"`
	ForkContext  *bool    `json:"fork_context,omitempty"`
	Resume       string   `json:"resume,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	InputFiles   []string `json:"input_files,omitempty"`
}

// New 创建 Task。
func New(deps Deps) (*Tool, error) {
	if deps.Catalog == nil || deps.Controller == nil || deps.BaseAgent == nil || deps.Approval == nil {
		return nil, fmt.Errorf("Task 依赖不完整")
	}
	source := deps.SnapshotSource
	if source == nil {
		source = contextsnapshot.ContextSource{}
	}
	return &Tool{deps: deps, snapshotSource: source}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	// ConcurrencySafe=true：同轮多个 Task 由 scheduler 并行；并发槽位由 Controller SlotLimiter 硬限。
	// ReadOnly=false：父会话会创建/更新子智能体实例，不等于「子代理只读」。
	return &tool.Info{Name: toolName, Description: "委派独立子智能体执行任务；resume 可基于已完成 agent_id 发起后续任务。", DescriptionFunc: t.description, Parameters: &tool.ParameterSchema{Type: "object", Properties: map[string]*tool.ParameterSchema{"subagent_type": {Type: "string", Description: "新建时来自 available_agents 的子智能体类型"}, "prompt": {Type: "string", Description: "给子智能体的完整、独立任务说明；resume 时为后续任务"}, "resume": {Type: "string", Description: "可选，已完成 Task 返回的 agent_id；存在时忽略新建定义字段"}, "description": {Type: "string", Description: "委派摘要，用于审批和进度"}, "run_in_background": {Type: "boolean", Description: "为 true 时立即返回 agent_id"}, "fork_context": {Type: "boolean", Description: "为 true 时传入经过过滤的父线程背景（resume 不适用）"}, "input_files": {Type: "array", Items: &tool.ParameterSchema{Type: "string"}, Description: "可选，带入子智能体沙箱的输入文件列表，如 [\"a.md\"]"}, "max_turns": {Type: "integer", Description: "子 Run 最大轮次，仅可收紧"}, "max_tokens": {Type: "integer", Description: "子 Run token 硬预算，仅可收紧"}, "max_tool_calls": {Type: "integer", Description: "子 Run 工具调用硬上限，仅可收紧"}, "timeout_seconds": {Type: "integer", Description: "子 Run 墙钟超时秒数，仅可收紧"}}, Required: []string{"prompt"}}, Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: true, NeedsPermission: true}}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolparam.Decode(params, &in); err != nil {
		return "", fmt.Errorf("解析 Task 参数失败: %w", err)
	}
	if err := validateInput(in); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Resume) != "" {
		return t.resume(ctx, in)
	}
	return t.Delegate(ctx, subagentcontract.DelegateRequest{
		SubagentType: in.SubagentType,
		Prompt:       in.Prompt,
		Description:  in.Description,
		Background:   in.Background,
		MaxTurns:     in.MaxTurns,
		MaxTokens:    in.MaxTokens,
		MaxToolCalls: in.MaxToolCalls,
		TimeoutSec:   in.TimeoutSec,
		ForkContext:  in.ForkContext,
		AllowedTools: in.AllowedTools,
		InputFiles:   in.InputFiles,
		PromptOrigin: "model",
	})
}

// Delegate 执行统一委派（Catalog 或显式 Definition）。
func (t *Tool) Delegate(ctx context.Context, req subagentcontract.DelegateRequest) (string, error) {
	if err := validateInput(input{
		MaxTurns: req.MaxTurns, MaxTokens: req.MaxTokens, MaxToolCalls: req.MaxToolCalls, TimeoutSec: req.TimeoutSec,
	}); err != nil {
		return "", err
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return "", fmt.Errorf("prompt 不能为空")
	}
	definition, err := t.resolveDefinition(req)
	if err != nil {
		return "", err
	}
	parentRunID, _ := contextutil.GetRunID(ctx)
	sessionID, _ := contextutil.GetSessionID(ctx)
	tenantID, _ := contextutil.GetTenantID(ctx)
	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{ToolName: toolName, Action: approvalmodel.ActionSubAgentDelegate, Resource: approvalmodel.Resource{Type: "subagent", URI: definition.Name, Display: definition.Name}, Reason: firstNonEmpty(req.Description, "委派子智能体"), Risk: approvalmodel.RiskMedium, Metadata: map[string]string{"subagent_type": definition.Name, "prompt_origin": firstNonEmpty(req.PromptOrigin, "model")}})
	if err != nil {
		return "", fmt.Errorf("Task 审批失败: %w", err)
	}
	if decision.Type != approvalmodel.DecisionApproved && decision.Type != approvalmodel.DecisionApprovedForScope {
		return "", fmt.Errorf("Task 未获授权: %s", firstNonEmpty(decision.Reason, string(decision.Type)))
	}
	currentDepth := contract.DelegationDepth(ctx)
	maxDepth := effectiveMaxDepth(ctx, definition.MaxDepth)
	if currentDepth >= maxDepth {
		return "", fmt.Errorf("agent depth limit reached: max=%d；本层不可再委派，请自行完成", maxDepth)
	}
	readOnly := definition.ReadOnly || contract.DelegationReadOnly(ctx)
	agent := t.childAgent(definition, readOnly, t.effectiveAllowedTools(ctx), currentDepth+1 < maxDepth)
	if len(req.AllowedTools) > 0 {
		agent.Tools = filterToolRefs(agent.Tools, req.AllowedTools)
	}
	if definition.MaxTurns > 0 {
		agent.RuntimePolicy.MaxIterations = stricterInt(agent.RuntimePolicy.MaxIterations, definition.MaxTurns)
	}
	if definition.MaxTokens > 0 {
		agent.RuntimePolicy.MaxTokens = stricterInt64(agent.RuntimePolicy.MaxTokens, definition.MaxTokens)
	}
	if definition.MaxToolCalls > 0 {
		agent.RuntimePolicy.MaxToolCalls = stricterInt(agent.RuntimePolicy.MaxToolCalls, definition.MaxToolCalls)
	}
	if req.MaxTurns > 0 {
		agent.RuntimePolicy.MaxIterations = stricterInt(agent.RuntimePolicy.MaxIterations, req.MaxTurns)
	}
	if req.MaxTokens > 0 {
		agent.RuntimePolicy.MaxTokens = stricterInt64(agent.RuntimePolicy.MaxTokens, req.MaxTokens)
	}
	if req.MaxToolCalls > 0 {
		agent.RuntimePolicy.MaxToolCalls = stricterInt(agent.RuntimePolicy.MaxToolCalls, req.MaxToolCalls)
	}
	caps := toolNames(agent.Tools)
	agent.SystemPrompt = subagentprompt.ComposeChildSystem(definition.SystemPrompt, subagentprompt.RuntimeContractInput{
		ReadOnly: readOnly, Capabilities: caps,
		MaxTurns: agent.RuntimePolicy.MaxIterations, MaxTokens: agent.RuntimePolicy.MaxTokens, MaxToolCalls: agent.RuntimePolicy.MaxToolCalls,
		PathFormat: "workspace-relative",
	})

	var inputSources []workmodel.ResourceRef
	if len(req.InputFiles) > 0 {
		var hints []string
		for _, f := range req.InputFiles {
			hints = append(hints, fmt.Sprintf("- %s", f))
		}
		req.Prompt += fmt.Sprintf("\n\n【关联输入文件通知】本任务关切的输入文件如下，请直接通过 read_file 工具读取对应文件名：\n%s", strings.Join(hints, "\n"))
		if t.deps.InputResolver != nil {
			if resolved, err := t.deps.InputResolver.ResolveAvailableInputs(ctx, req.InputFiles); err == nil {
				inputSources = resolved
			}
		}
	}

	mode, parentMessages, toolCallID, err := t.resolveSnapshot(ctx, definition, req)
	if err != nil {
		return "", err
	}
	delegation, err := contextsnapshot.Builder{}.Build(contextsnapshot.Input{
		Mode:     mode,
		Messages: parentMessages,
		MaxRunes: maxDelegationInputRunes,
		Delegation: contextsnapshot.DelegationEnvelope{
			ParentRunID:    parentRunID,
			ToolCallID:     toolCallID,
			PromptOrigin:   firstNonEmpty(req.PromptOrigin, "model"),
			Objective:      strings.TrimSpace(req.Prompt),
			ExpectedOutput: subagentprompt.DefaultExpectedOutput,
			Capabilities:   caps,
			MaxTurns:       agent.RuntimePolicy.MaxIterations,
			MaxTokens:      agent.RuntimePolicy.MaxTokens,
			MaxToolCalls:   agent.RuntimePolicy.MaxToolCalls,
			ReturnContract: subagentprompt.DefaultReturnContract,
		},
	})
	if err != nil {
		return "", fmt.Errorf("构造子智能体输入失败: %w", err)
	}
	spawnCtx := contextsnapshot.WithoutParentSnapshot(ctx)
	if parentSink := progress.FromContext(ctx); parentSink != nil {
		subName := definition.Name
		subDepth := currentDepth + 1
		childSink := func(ev progress.Event) {
			if ev.Depth <= 0 {
				ev.Depth = subDepth
			}
			if ev.SubAgentID == "" {
				ev.SubAgentID = subName
			}
			if !strings.HasPrefix(ev.Summary, "[Sub-Agent") {
				ev.Summary = fmt.Sprintf("[Sub-Agent: %s] %s", subName, ev.Summary)
			}
			parentSink(ev)
		}
		// 用 ChildBridge 而非 WithSink：Controller 会清掉父 Sink，只认显式桥接。
		spawnCtx = progress.WithChildBridge(spawnCtx, childSink)
	}
	timeout := stricterDuration(time.Duration(definition.TimeoutSec)*time.Second, time.Duration(req.TimeoutSec)*time.Second)
	budget := contract.TreeBudgetFromContext(ctx)
	if budget == nil {
		budget = contract.NewTreeBudget(agent.RuntimePolicy.MaxTokens, agent.RuntimePolicy.MaxToolCalls)
	}
	spawnCtx = contract.WithTreeBudget(spawnCtx, budget)
	instance, err := t.deps.Controller.Spawn(spawnCtx, contract.SpawnRequest{
		SessionID: sessionID, TenantID: tenantID, ParentRunID: parentRunID,
		Depth: currentDepth + 1, MaxDepth: maxDepth, ReadOnly: readOnly,
		SubagentType: definition.Name, Prompt: delegation.UserInput, Agent: agent,
		Timeout: timeout, Budget: budget, Inputs: inputSources,
		SkillQAPolicy: req.SkillQAPolicy, SkillQAEnforcement: req.SkillQAEnforcement,
	})
	if err != nil {
		return "", err
	}
	if definition.ExecutionMode == subagentmodel.ExecutionModeAsync || req.Background {
		t.startBackground(instance.AgentID)
		return encode(model.TaskLaunch{Status: "async_launched", AgentID: instance.AgentID, ChildRunID: instance.ChildRunID})
	}
	instance, err = t.deps.Controller.Wait(ctx, instance.AgentID)
	if err != nil {
		return "", err
	}
	if instance.Result != nil {
		return encode(*instance.Result)
	}
	return encode(model.TaskResult{SchemaVersion: 1, Status: model.ResultStatusFailed, AgentID: instance.AgentID, Error: &model.ResultError{Code: "missing_result", Message: "子智能体未产生可交付结果", Retryable: true}})
}

func (t *Tool) resolveDefinition(req subagentcontract.DelegateRequest) (subagentmodel.Definition, error) {
	if req.Definition != nil {
		definition := *req.Definition
		definition.Name = strings.TrimSpace(definition.Name)
		if definition.Name == "" {
			return subagentmodel.Definition{}, fmt.Errorf("临时 SubAgent Definition 缺少 name")
		}
		return definition, nil
	}
	definition, ok := t.deps.Catalog.Get(req.SubagentType)
	if !ok {
		return subagentmodel.Definition{}, fmt.Errorf("未知 subagent_type %q，请从 available_agents 中选择", req.SubagentType)
	}
	return definition, nil
}

func (t *Tool) resolveSnapshot(ctx context.Context, definition subagentmodel.Definition, req subagentcontract.DelegateRequest) (contextsnapshot.Mode, []*domain.Message, string, error) {
	if mode := mapSnapshotMode(req.SnapshotMode); mode == contextsnapshot.ModeSkillIsolated {
		return contextsnapshot.ModeSkillIsolated, nil, "", nil
	}
	forkContext := definition.ForkContext != nil && *definition.ForkContext
	if req.ForkContext != nil {
		forkContext = *req.ForkContext
	}
	if !forkContext {
		if mode := mapSnapshotMode(req.SnapshotMode); mode != "" {
			return mode, nil, "", nil
		}
		return contextsnapshot.ModeIsolated, nil, "", nil
	}
	snapshot, err := t.snapshotSource.Snapshot(ctx)
	if err != nil {
		return "", nil, "", fmt.Errorf("读取父 transcript 快照失败: %w", err)
	}
	if strings.TrimSpace(snapshot.ToolCallID) == "" {
		return "", nil, "", fmt.Errorf("读取父 transcript 快照失败: 缺少当前 Task 调用标识")
	}
	return contextsnapshot.ModeFilteredHistory, snapshot.Messages, snapshot.ToolCallID, nil
}

func (t *Tool) resume(ctx context.Context, in input) (string, error) {
	parentRunID, _ := contextutil.GetRunID(ctx)
	sessionID, _ := contextutil.GetSessionID(ctx)
	tenantID, _ := contextutil.GetTenantID(ctx)
	previous, err := t.deps.Controller.Get(ctx, strings.TrimSpace(in.Resume))
	if err != nil {
		return "", err
	}
	if previous.ParentRunID != parentRunID || previous.SessionID != sessionID || previous.TenantID != tenantID {
		return "", fmt.Errorf("无权 resume 其他父 Run 的子智能体")
	}
	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{ToolName: toolName, Action: approvalmodel.ActionSubAgentDelegate, Resource: approvalmodel.Resource{Type: "subagent", URI: previous.SubagentType, Display: previous.SubagentType}, Reason: firstNonEmpty(in.Description, "继续子智能体任务"), Risk: approvalmodel.RiskMedium, Metadata: map[string]string{"subagent_type": previous.SubagentType, "resume": previous.AgentID}})
	if err != nil {
		return "", fmt.Errorf("Task resume 审批失败: %w", err)
	}
	if decision.Type != approvalmodel.DecisionApproved && decision.Type != approvalmodel.DecisionApprovedForScope {
		return "", fmt.Errorf("Task resume 未获授权: %s", firstNonEmpty(decision.Reason, string(decision.Type)))
	}
	instance, err := t.deps.Controller.Resume(contextsnapshot.WithoutParentSnapshot(ctx), previous.AgentID, in.Prompt)
	if err != nil {
		return "", err
	}
	background := in.Background
	if definition, ok := t.deps.Catalog.Get(previous.SubagentType); ok && definition.ExecutionMode == subagentmodel.ExecutionModeAsync {
		background = true
	}
	if background {
		t.startBackground(instance.AgentID)
		return encode(model.TaskLaunch{Status: "async_launched", AgentID: instance.AgentID, ChildRunID: instance.ChildRunID})
	}
	instance, err = t.deps.Controller.Wait(ctx, instance.AgentID)
	if err != nil {
		return "", err
	}
	if instance.Result != nil {
		return encode(*instance.Result)
	}
	return encode(model.TaskResult{SchemaVersion: 1, Status: model.ResultStatusFailed, AgentID: instance.AgentID, Error: &model.ResultError{Code: "missing_result", Message: "子智能体未产生可交付结果", Retryable: true}})
}

func encode(value any) (string, error) {
	if tr, ok := value.(model.TaskResult); ok {
		encoded, err := json.Marshal(tr.ToCompact())
		if err != nil {
			return "", fmt.Errorf("编码 Task 结果失败: %w", err)
		}
		return string(encoded), nil
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("编码 Task 结果失败: %w", err)
	}
	return string(encoded), nil
}

func validateInput(in input) error {
	if in.MaxTurns < 0 {
		return fmt.Errorf("max_turns 不能小于 0")
	}
	if in.MaxTokens < 0 {
		return fmt.Errorf("max_tokens 不能小于 0")
	}
	if in.MaxToolCalls < 0 {
		return fmt.Errorf("max_tool_calls 不能小于 0")
	}
	if in.TimeoutSec < 0 {
		return fmt.Errorf("timeout_seconds 不能小于 0")
	}
	return nil
}

func toolNames(tools []domain.ToolRef) []string {
	names := make([]string, 0, len(tools))
	for _, toolRef := range tools {
		names = append(names, toolRef.Name)
	}
	return names
}

func filterToolRefs(tools []domain.ToolRef, allowed []string) []domain.ToolRef {
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		if name = strings.TrimSpace(name); name != "" {
			set[name] = struct{}{}
		}
	}
	filtered := make([]domain.ToolRef, 0, len(tools))
	for _, toolRef := range tools {
		if _, ok := set[toolRef.Name]; ok {
			filtered = append(filtered, toolRef)
		}
	}
	return filtered
}

func mapSnapshotMode(raw string) contextsnapshot.Mode {
	switch strings.TrimSpace(raw) {
	case subagentcontract.SnapshotModeIsolated:
		return contextsnapshot.ModeIsolated
	case subagentcontract.SnapshotModeLastNTurns:
		return contextsnapshot.ModeLastNTurns
	case subagentcontract.SnapshotModeFilteredHistory:
		return contextsnapshot.ModeFilteredHistory
	case subagentcontract.SnapshotModeSkillIsolated:
		return contextsnapshot.ModeSkillIsolated
	default:
		return ""
	}
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

func stricterDuration(current, requested time.Duration) time.Duration {
	if current == 0 || (requested > 0 && requested < current) {
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
	return service.RenderDescription(t.deps.Catalog, service.DescriptionOptions{
		Posture:       string(subagentprompt.NormalizePosture(t.deps.DelegationPosture)),
		MaxConcurrent: t.deps.MaxConcurrent,
	})
}

func (t *Tool) startBackground(agentID string) {
	if t.deps.Background == nil {
		return
	}
	go func() {
		_ = t.deps.Background.Run(context.Background(), agentID)
	}()
}

func (t *Tool) childAgent(definition subagentmodel.Definition, readOnly bool, parentTools []string, allowDelegation bool) *domain.Agent {
	clone := *t.deps.BaseAgent
	clone.ID = "subagent-" + clone.ID
	clone.Name = "SubAgent"
	clone.SystemPrompt = "" // 由 ComposeChildSystem 写入
	clone.Tools = make([]domain.ToolRef, 0, len(t.deps.AllowedTools))
	allowed := parentTools
	if len(definition.Tools) > 0 {
		allowed = intersect(allowed, definition.Tools)
	}
	// 只有处于沙箱环境（如 skill-fork 派生或声明了 run_skill_command）的子智能体才需要剔除宿主机高危 run_command
	isSandboxed := strings.HasPrefix(definition.Name, "skill-fork:") || containsString(definition.Tools, "run_skill_command") || containsString(definition.Tools, "sandbox_exec")

	for _, name := range allowed {
		if name == "TaskOutput" || name == "TaskStop" || name == "Skill" {
			continue
		}
		if name == "run_command" && isSandboxed {
			continue
		}
		if name == toolName && !allowDelegation {
			continue
		}
		if readOnly && !isReadOnly(name) {
			continue
		}
		clone.Tools = append(clone.Tools, domain.ToolRef{Name: name})
	}
	return &clone
}

func (t *Tool) effectiveAllowedTools(ctx context.Context) []string {
	parentTools := append([]string(nil), t.deps.AllowedTools...)
	if inherited, ok := contract.DelegationTools(ctx); ok {
		parentTools = intersect(parentTools, inherited)
	}
	return parentTools
}

func effectiveMaxDepth(ctx context.Context, definitionMax int) int {
	maxDepth := contract.MaxDelegationDepth(ctx)
	if maxDepth <= 0 && definitionMax > 0 {
		maxDepth = definitionMax
	}
	if maxDepth <= 0 {
		maxDepth = 1
	}
	if contract.MaxDelegationDepth(ctx) > 0 && definitionMax > 0 && definitionMax < maxDepth {
		maxDepth = definitionMax
	}
	return maxDepth
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

func containsString(slice []string, target string) bool {
	for _, item := range slice {
		if item == target {
			return true
		}
	}
	return false
}
