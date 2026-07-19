// Package skill 实现 Skill 网关工具。
package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	subagentcontract "genesis-agent/internal/capabilities/subagent/contract"
	subagentmodel "genesis-agent/internal/capabilities/subagent/model"
	subagentprompt "genesis-agent/internal/capabilities/subagent/prompt"
	tool "genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
)

const (
	toolNameSkill     = "Skill"
	staticDescription = "加载已发现 Skill 的完整说明。参数 skill 必须来自本工具 description 中的 <available_skills>。禁止把技能名当作独立工具调用。"
)

// Deps 描述 Skill 网关依赖。
type Deps struct {
	Service        skillcontract.Service
	Approval       approvalcontract.Service
	CatalogRequest skillcontract.CatalogRequest
	EnabledTools   []string
}

// Tool 是 Skill 网关。
type Tool struct {
	deps     Deps
	forkTask tool.Tool
}

// SetForkTask 由共享 bootstrap 在 Task 创建后注入唯一委派网关。
// Skill 不直接依赖 Controller 或具体运行策略；fork 优先走 Delegator。
func (t *Tool) SetForkTask(task tool.Tool) { t.forkTask = task }

type input struct {
	Skill    string `json:"skill,omitempty"`
	Resource string `json:"resource,omitempty"`
	Args     string `json:"args,omitempty"`
}

type dependencyOutput struct {
	Type        string `json:"type"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
}

// output 是短确认（不含完整 SKILL.md body）；body 由 runtime 单份 injection。
type output struct {
	Type              string             `json:"type"`
	Name              string             `json:"name"`
	QualifiedName     string             `json:"qualified_name"`
	Resource          string             `json:"resource"`
	Content           string             `json:"content,omitempty"` // 供 runtime 解析后剥离；模型侧应只看短确认
	Args              string             `json:"args,omitempty"`
	Truncated         bool               `json:"truncated"`
	AllowedTools      []string           `json:"allowed_tools,omitempty"`
	Context           string             `json:"context,omitempty"`
	Agent             string             `json:"agent,omitempty"`
	Model             string             `json:"model,omitempty"`
	MaxThinkingTokens int                `json:"max_thinking_tokens,omitempty"`
	Dependencies      []dependencyOutput `json:"dependencies,omitempty"`
}

// New 创建 Skill 网关工具。
func New(deps Deps) (tool.Tool, error) {
	if deps.Service == nil {
		return nil, fmt.Errorf("skill service不能为空")
	}
	if deps.Approval == nil {
		return nil, fmt.Errorf("approval service不能为空")
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:            toolNameSkill,
		Description:     staticDescription,
		DescriptionFunc: t.renderDescription,
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"skill":    {Type: "string", Description: "Skill 名称或 qualified_name，必须来自 <available_skills>，例如 office-ppt"},
				"resource": {Type: "string", Description: "可选，不透明 resource id，用于消除同名冲突"},
				"args":     {Type: "string", Description: "传给 Skill 的用户参数或任务上下文"},
			},
			Required: []string{},
		},
		Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false, NeedsPermission: true, RequiresUserInteraction: true},
	}
}

func (t *Tool) renderDescription(ctx context.Context) (string, error) {
	catalog, err := t.deps.Service.RenderAvailableSkills(ctx, t.deps.CatalogRequest)
	if err != nil {
		return staticDescription, err
	}
	catalog = strings.TrimSpace(catalog)
	if catalog == "" {
		return staticDescription + "\n\n<available_skills>\n</available_skills>", nil
	}
	var sb strings.Builder
	sb.WriteString(staticDescription)
	sb.WriteString("\n\n<skills_instructions>\n")
	sb.WriteString("当任务匹配可用技能时，必须先调用 Skill 工具加载该技能，再使用原语工具执行。\n")
	sb.WriteString("禁止把技能名当作独立工具名调用。例如禁止调用 office-ppt；正确做法是 Skill(skill=\"office-ppt\")。\n")
	sb.WriteString("</skills_instructions>\n\n")
	sb.WriteString(catalog)
	return sb.String(), nil
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolparam.Decode(params, &in); err != nil {
		return "", fmt.Errorf("解析Skill参数失败（参数仅支持 skill/resource/args，必须提供 skill或resource）: %w", err)
	}
	return t.load(ctx, in, true, "model")
}

// LoadExplicitSkill 加载用户显式选择的 Skill。
// 这是 runtime 内部端口，不进入 LLM schema；它复用网关的依赖预检、审批和注入输出。
func (t *Tool) LoadExplicitSkill(ctx context.Context, req skillcontract.ExplicitLoadRequest) (string, error) {
	return t.load(ctx, input{Skill: req.Skill, Resource: req.Resource, Args: req.Args}, false, "explicit")
}

func (t *Tool) load(ctx context.Context, in input, modelCall bool, invocation string) (string, error) {
	skillName := strings.TrimSpace(in.Skill)
	if skillName == "" && strings.TrimSpace(in.Resource) == "" {
		return "", fmt.Errorf("skill或resource必须至少提供一个")
	}
	resolveReq := skillcontract.ResolveRequest{
		CatalogRequest: t.deps.CatalogRequest,
		Name:           skillName,
		Resource:       strings.TrimSpace(in.Resource),
		ModelCall:      modelCall,
		Invocation:     invocation,
	}
	meta, err := t.deps.Service.Resolve(ctx, resolveReq)
	if err != nil {
		return "", err
	}
	if modelCall {
		if dispatcher := hookcontract.FromContext(ctx); dispatcher != nil {
			result, hookErr := dispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventPreSkillUse, MatchKey: meta.QualifiedName, Payload: map[string]any{"skill_name": meta.QualifiedName, "invocation": invocation}})
			if hookErr != nil {
				return "", fmt.Errorf("执行 PreSkillUse Hook 失败: %w", hookErr)
			}
			hookcontract.AppendAdditionalContext(ctx, result.AdditionalContext...)
			if result.Blocked {
				return "", fmt.Errorf("Skill %q 被 Hook 阻断: %s", meta.QualifiedName, result.BlockReason)
			}
		}
	}
	depReport, err := t.checkDependencies(ctx, meta, invocation)
	if err != nil {
		return "", err
	}
	if err := t.authorize(ctx, meta, depReport, invocation); err != nil {
		return "", err
	}
	if defaultContext(meta.Context) == model.ContextModeFork {
		return t.fork(ctx, meta, resolveReq, in.Args)
	}
	injection, err := t.deps.Service.Load(ctx, skillcontract.LoadRequest{ResolveRequest: resolveReq, Args: in.Args})
	if err != nil {
		return "", err
	}
	if modelCall {
		if dispatcher := hookcontract.FromContext(ctx); dispatcher != nil {
			result, hookErr := dispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventPostSkillUse, MatchKey: injection.Skill.QualifiedName, Payload: map[string]any{"skill_name": injection.Skill.QualifiedName, "invocation": invocation, "injected": true}})
			if hookErr != nil {
				return "", fmt.Errorf("执行 PostSkillUse Hook 失败: %w", hookErr)
			}
			hookcontract.AppendAdditionalContext(ctx, result.AdditionalContext...)
		}
	}
	// Content 仍放入 JSON 供 runtime parseSkillInjection；react_loop 会改为短 ToolResult + 单份 system injection。
	return toJSON(output{
		Type:              "skill_injection",
		Name:              injection.Skill.Name,
		QualifiedName:     injection.Skill.QualifiedName,
		Resource:          string(injection.Resource),
		Content:           injection.Contents,
		Args:              injection.Args,
		Truncated:         injection.Truncated,
		AllowedTools:      cloneStrings(injection.Skill.AllowedTools),
		Context:           string(defaultContext(injection.Skill.Context)),
		Agent:             injection.Skill.Agent,
		Model:             injection.Skill.Model,
		MaxThinkingTokens: injection.Skill.MaxThinkingTokens,
		Dependencies:      depReport.Outputs,
	})
}

func (t *Tool) fork(ctx context.Context, meta model.Metadata, resolveReq skillcontract.ResolveRequest, args string) (string, error) {
	if t.forkTask == nil {
		return "", fmt.Errorf("skill %q 声明 context=fork，但 Task 委派网关未注入", meta.QualifiedName)
	}
	delegator, ok := t.forkTask.(subagentcontract.Delegator)
	if !ok {
		return "", fmt.Errorf("skill %q 的 Task 委派网关未实现 Delegator", meta.QualifiedName)
	}
	// fork 的参数由委派信封统一追加，避免 Skill.Load 与 Task.prompt 双重注入。
	injection, err := t.deps.Service.Load(ctx, skillcontract.LoadRequest{ResolveRequest: resolveReq})
	if err != nil {
		return "", err
	}
	body := strings.TrimSpace(injection.Contents)
	if body == "" {
		return "", fmt.Errorf("skill %q 的 fork 内容为空", meta.QualifiedName)
	}
	args = strings.TrimSpace(args)
	agentType := strings.TrimSpace(meta.Agent)
	req := subagentcontract.DelegateRequest{
		Description:  "执行 Skill " + meta.QualifiedName,
		AllowedTools: cloneStrings(meta.AllowedTools),
		PromptOrigin: "skill_fork",
	}
	// Skill fork 一律 skill_isolated：不复制父聊天；正文进 definition/prompt，不注入主线程。
	req.SnapshotMode = subagentcontract.SnapshotModeSkillIsolated
	if agentType != "" {
		// 命名 agent 必须来自 Catalog；Skill 正文进入 prompt，不覆盖 Definition system。
		req.SubagentType = agentType
		req.Prompt = body
		if args != "" {
			req.Prompt += "\n\n任务参数：\n" + args
		}
	} else {
		// 无 agent：合成临时 Definition（不进用户 Catalog），body 作 system，args 作委派输入。
		req.Definition = &subagentmodel.Definition{
			Name:         subagentprompt.SkillForkDefinitionName(meta.QualifiedName),
			Description:  "Skill fork: " + meta.QualifiedName,
			WhenToUse:    "由 Skill(context=fork) 硬编码 Spawn",
			SystemPrompt: body,
			Tools:        cloneStrings(meta.AllowedTools),
		}
		if args != "" {
			req.Prompt = args
		} else {
			req.Prompt = "按技能说明完成被委派的任务。"
		}
	}
	return delegator.Delegate(ctx, req)
}

type dependencyReport struct {
	Outputs          []dependencyOutput
	ExternalCount    int
	UnavailableTools []string
	RequiresApproval bool
	ApprovalReason   string
	DependencyCount  int
}

func (t *Tool) checkDependencies(ctx context.Context, meta model.Metadata, invocation string) (dependencyReport, error) {
	report := dependencyReport{}
	availableTools := stringSet(normalizeEnabledTools(t.deps.EnabledTools))
	hasToolInventory := len(t.deps.EnabledTools) > 0
	for _, dep := range meta.Dependencies.Tools {
		depType := strings.TrimSpace(strings.ToLower(dep.Type))
		if depType == "" {
			depType = "tool"
		}
		value := strings.TrimSpace(dep.Value)
		if value == "" {
			continue
		}
		report.DependencyCount++
		out := dependencyOutput{Type: depType, Value: value, Description: dep.Description, Status: "available"}
		switch depType {
		case "tool":
			if !hasToolInventory {
				out.Status = "missing"
				report.UnavailableTools = append(report.UnavailableTools, value)
			} else if _, ok := availableTools[value]; !ok {
				out.Status = "missing"
				report.UnavailableTools = append(report.UnavailableTools, value)
			}
		case "mcp", "connection", "external", "command", "url":
			out.Status = "requires_approval"
			report.ExternalCount++
			report.RequiresApproval = true
		default:
			out.Status = "requires_approval"
			report.ExternalCount++
			report.RequiresApproval = true
		}
		report.Outputs = append(report.Outputs, out)
	}
	if len(report.UnavailableTools) > 0 {
		return report, fmt.Errorf("skill %q 依赖未启用工具: %s", meta.QualifiedName, strings.Join(report.UnavailableTools, ", "))
	}
	if report.RequiresApproval {
		report.ApprovalReason = "Skill声明了外部依赖，需要确认后才能加载"
		decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{
			ToolName: toolNameSkill,
			Action:   approvalmodel.ActionSkillLoad,
			Resource: approvalmodel.Resource{
				Type:     "skill.dependencies",
				URI:      model.SkillDependenciesDecisionKey(meta.QualifiedName),
				Display:  model.SkillDependenciesDecisionKey(meta.QualifiedName),
				Metadata: dependencyApprovalMetadata(meta, report, invocation),
			},
			Reason:          report.ApprovalReason,
			Risk:            approvalmodel.RiskMedium,
			SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce, approvalmodel.GrantScopeSession},
		})
		if err != nil {
			return report, err
		}
		switch decision.Type {
		case approvalmodel.DecisionApproved, approvalmodel.DecisionApprovedForScope:
			return report, nil
		default:
			reason := strings.TrimSpace(decision.Reason)
			if reason == "" {
				reason = "skill dependencies require approval"
			}
			return report, fmt.Errorf("Skill依赖未通过审批: %s", reason)
		}
	}
	return report, nil
}

func (t *Tool) authorize(ctx context.Context, meta model.Metadata, deps dependencyReport, invocation string) error {
	metadata := map[string]string{
		"trusted":                   "true",
		"authority":                 meta.Authority.String(),
		"package":                   string(meta.PackageID),
		"qualified_name":            meta.QualifiedName,
		"dependency_count":          fmt.Sprintf("%d", deps.DependencyCount),
		"external_dependency_count": fmt.Sprintf("%d", deps.ExternalCount),
	}
	addInvocationMetadata(metadata, invocation)
	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{
		ToolName: toolNameSkill,
		Action:   approvalmodel.ActionSkillLoad,
		Resource: approvalmodel.Resource{
			Type:     "skill",
			URI:      model.SkillDecisionKey(meta.QualifiedName),
			Display:  model.SkillDecisionKey(meta.QualifiedName),
			Metadata: metadata,
		},
		Reason: "加载Skill上下文",
		Risk:   approvalmodel.RiskLow,
	})
	if err != nil {
		return err
	}
	switch decision.Type {
	case approvalmodel.DecisionApproved, approvalmodel.DecisionApprovedForScope:
		return nil
	default:
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "skill load denied"
		}
		return fmt.Errorf("Skill未通过审批: %s", reason)
	}
}

func dependencyApprovalMetadata(meta model.Metadata, report dependencyReport, invocation string) map[string]string {
	metadata := map[string]string{
		"trusted":                   "false",
		"dangerous":                 "true",
		"authority":                 meta.Authority.String(),
		"package":                   string(meta.PackageID),
		"qualified_name":            meta.QualifiedName,
		"dependency_count":          fmt.Sprintf("%d", report.DependencyCount),
		"external_dependency_count": fmt.Sprintf("%d", report.ExternalCount),
	}
	addInvocationMetadata(metadata, invocation)
	return metadata
}

func addInvocationMetadata(metadata map[string]string, invocation string) {
	invocation = strings.TrimSpace(invocation)
	if invocation == "" {
		return
	}
	metadata["invocation"] = invocation
}

func toJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func normalizeEnabledTools(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func defaultContext(value model.ContextMode) model.ContextMode {
	if value == "" {
		return model.ContextModeInline
	}
	return value
}
