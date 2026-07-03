// Package load_skill 实现 load_skill 工具。
package load_skill

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

// Deps 描述 load_skill 依赖。
type Deps struct {
	Service        skillcontract.Service
	Approval       approvalcontract.Service
	CatalogRequest skillcontract.CatalogRequest
	EnabledTools   []string
}

type Tool struct {
	deps Deps
}

type input struct {
	Name     string `json:"name,omitempty"`
	Resource string `json:"resource,omitempty"`
	Args     string `json:"args,omitempty"`
}

type dependencyOutput struct {
	Type        string `json:"type"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
}
type output struct {
	Type              string             `json:"type"`
	Name              string             `json:"name"`
	QualifiedName     string             `json:"qualified_name"`
	Resource          string             `json:"resource"`
	Content           string             `json:"content"`
	Args              string             `json:"args,omitempty"`
	Truncated         bool               `json:"truncated"`
	AllowedTools      []string           `json:"allowed_tools,omitempty"`
	Context           string             `json:"context,omitempty"`
	Agent             string             `json:"agent,omitempty"`
	Model             string             `json:"model,omitempty"`
	MaxThinkingTokens int                `json:"max_thinking_tokens,omitempty"`
	Dependencies      []dependencyOutput `json:"dependencies,omitempty"`
}

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
		Name:        "load_skill",
		Description: "加载已发现 Skill 的完整说明。先根据系统提示中的 Skills 列表选择 name；如名称冲突，使用 resource。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"name":     {Type: "string", Description: "Skill 名称或 qualified_name，例如 review-fix-rereview"},
				"resource": {Type: "string", Description: "可选，不透明 resource id，用于消除同名冲突"},
				"args":     {Type: "string", Description: "传给 Skill 的用户参数或任务上下文"},
			},
		},
		Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false, NeedsPermission: true, RequiresUserInteraction: true},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析load_skill参数失败: %w", err)
	}
	if strings.TrimSpace(in.Name) == "" && strings.TrimSpace(in.Resource) == "" {
		return "", fmt.Errorf("name或resource必须至少提供一个")
	}
	resolveReq := skillcontract.ResolveRequest{CatalogRequest: t.deps.CatalogRequest, Name: strings.TrimSpace(in.Name), Resource: strings.TrimSpace(in.Resource), ModelCall: true}
	meta, err := t.deps.Service.Resolve(ctx, resolveReq)
	if err != nil {
		return "", err
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
	return toJSON(output{Type: "skill_injection", Name: injection.Skill.Name, QualifiedName: injection.Skill.QualifiedName, Resource: string(injection.Resource), Content: injection.Contents, Args: injection.Args, Truncated: injection.Truncated, AllowedTools: cloneStrings(injection.Skill.AllowedTools), Context: string(defaultContext(injection.Skill.Context)), Agent: injection.Skill.Agent, Model: injection.Skill.Model, MaxThinkingTokens: injection.Skill.MaxThinkingTokens, Dependencies: depReport.Outputs})
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
	availableTools := stringSet(t.deps.EnabledTools)
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
		decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{ToolName: "load_skill", Action: approvalmodel.ActionSkillLoad, Resource: approvalmodel.Resource{Type: "skill.dependencies", URI: meta.Authority.String() + ":" + string(meta.PackageID) + ":dependencies", Display: meta.QualifiedName, Metadata: map[string]string{"trusted": "false", "dangerous": "true", "dependency_count": fmt.Sprintf("%d", report.DependencyCount), "external_dependency_count": fmt.Sprintf("%d", report.ExternalCount)}}, Reason: report.ApprovalReason, Risk: approvalmodel.RiskMedium, SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce, approvalmodel.GrantScopeSession}})
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
			return report, fmt.Errorf("load_skill依赖未通过审批: %s", reason)
		}
	}
	return report, nil
}

func markApprovedDependencies(report *dependencyReport) {
	if report == nil {
		return
	}
	for i := range report.Outputs {
		if report.Outputs[i].Status == "requires_approval" {
			report.Outputs[i].Status = "approved"
		}
	}
}
func (t *Tool) authorize(ctx context.Context, meta model.Metadata, deps dependencyReport) error {
	metadata := map[string]string{"trusted": "true", "authority": meta.Authority.String(), "package": string(meta.PackageID), "dependency_count": fmt.Sprintf("%d", deps.DependencyCount), "external_dependency_count": fmt.Sprintf("%d", deps.ExternalCount)}
	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{ToolName: "load_skill", Action: approvalmodel.ActionSkillLoad, Resource: approvalmodel.Resource{Type: "skill", URI: meta.Authority.String() + ":" + string(meta.PackageID), Display: meta.QualifiedName, Metadata: metadata}, Reason: "加载Skill上下文", Risk: approvalmodel.RiskLow})
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
		return fmt.Errorf("load_skill未通过审批: %s", reason)
	}
}
func toJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
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
