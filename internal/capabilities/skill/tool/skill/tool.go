// Package skill 实现 Skill 网关工具。
package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
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
	deps Deps
}

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
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析Skill参数失败: %w", err)
	}
	skillName := strings.TrimSpace(in.Skill)
	if skillName == "" && strings.TrimSpace(in.Resource) == "" {
		return "", fmt.Errorf("skill或resource必须至少提供一个")
	}
	resolveReq := skillcontract.ResolveRequest{
		CatalogRequest: t.deps.CatalogRequest,
		Name:           skillName,
		Resource:       strings.TrimSpace(in.Resource),
		ModelCall:      true,
	}
	meta, err := t.deps.Service.Resolve(ctx, resolveReq)
	if err != nil {
		return "", err
	}
	if defaultContext(meta.Context) == model.ContextModeFork {
		// fork 语义预留给后续规范化 subagent 运行时：独立上下文、收窄工具/Skill/MCP、结果回传主线程。
		// 当前不实现半吊子子 Run，避免与后续多 Agent / 子 Agent 设计冲突。
		return "", fmt.Errorf("skill %q 声明 context=fork；规范化 subagent 运行时尚未启用，请改用 context=inline", meta.QualifiedName)
	}
	depReport, err := t.checkDependencies(ctx, meta)
	if err != nil {
		return "", err
	}
	if err := t.authorize(ctx, meta, depReport); err != nil {
		return "", err
	}
	injection, err := t.deps.Service.Load(ctx, skillcontract.LoadRequest{ResolveRequest: resolveReq, Args: in.Args})
	if err != nil {
		return "", err
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

type dependencyReport struct {
	Outputs          []dependencyOutput
	ExternalCount    int
	UnavailableTools []string
	RequiresApproval bool
	ApprovalReason   string
	DependencyCount  int
}

func (t *Tool) checkDependencies(ctx context.Context, meta model.Metadata) (dependencyReport, error) {
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
				Type:    "skill.dependencies",
				URI:     model.SkillDependenciesDecisionKey(meta.QualifiedName),
				Display: model.SkillDependenciesDecisionKey(meta.QualifiedName),
				Metadata: map[string]string{
					"trusted":                   "false",
					"dangerous":                 "true",
					"authority":                 meta.Authority.String(),
					"package":                   string(meta.PackageID),
					"qualified_name":            meta.QualifiedName,
					"dependency_count":          fmt.Sprintf("%d", report.DependencyCount),
					"external_dependency_count": fmt.Sprintf("%d", report.ExternalCount),
				},
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

func (t *Tool) authorize(ctx context.Context, meta model.Metadata, deps dependencyReport) error {
	metadata := map[string]string{
		"trusted":                   "true",
		"authority":                 meta.Authority.String(),
		"package":                   string(meta.PackageID),
		"qualified_name":            meta.QualifiedName,
		"dependency_count":          fmt.Sprintf("%d", deps.DependencyCount),
		"external_dependency_count": fmt.Sprintf("%d", deps.ExternalCount),
	}
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
