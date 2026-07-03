// Package search_skill_resources 实现 Skill 包内资源搜索工具。
package search_skill_resources

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

type Deps struct {
	Service        skillcontract.Service
	Approval       approvalcontract.Service
	CatalogRequest skillcontract.CatalogRequest
}

type Tool struct{ deps Deps }

type input struct {
	Name    string `json:"name,omitempty"`
	Package string `json:"package,omitempty"`
	Query   string `json:"query"`
	Limit   int    `json:"limit,omitempty"`
}

type output struct {
	SkillQualifiedName string              `json:"skill_qualified_name"`
	Matches            []model.SearchMatch `json:"matches"`
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
	return &tool.Info{Name: "search_skill_resources", Description: "搜索 Skill 包内 references/assets/scripts 文本资源，返回 resource id 和片段。", Parameters: &tool.ParameterSchema{Type: "object", Properties: map[string]*tool.ParameterSchema{"name": {Type: "string", Description: "Skill 名称或 qualified_name"}, "package": {Type: "string", Description: "可选 package id"}, "query": {Type: "string", Description: "搜索关键词"}, "limit": {Type: "integer", Description: "最大返回数量"}}, Required: []string{"query"}}, Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: true, ConcurrencySafe: true, NeedsPermission: true}}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析search_skill_resources参数失败: %w", err)
	}
	if strings.TrimSpace(in.Query) == "" {
		return "", fmt.Errorf("query不能为空")
	}
	pkg := model.PackageID(strings.TrimSpace(in.Package))
	if err := t.authorize(ctx, pkg, in.Query); err != nil {
		return "", err
	}
	result, err := t.deps.Service.SearchResources(ctx, skillcontract.SearchResourcesRequest{ResolveRequest: skillcontract.ResolveRequest{CatalogRequest: t.deps.CatalogRequest, Name: strings.TrimSpace(in.Name)}, PackageID: pkg, Query: in.Query, Limit: in.Limit})
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(output{SkillQualifiedName: result.Skill.QualifiedName, Matches: result.Matches})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *Tool) authorize(ctx context.Context, pkg model.PackageID, query string) error {
	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{ToolName: "search_skill_resources", Action: approvalmodel.ActionSkillResourceRead, Resource: approvalmodel.Resource{Type: "skill_resource_search", URI: string(pkg) + ":search", Display: query, Metadata: map[string]string{"trusted": "true", "package": string(pkg)}}, Reason: "搜索Skill包内资源", Risk: approvalmodel.RiskLow})
	if err != nil {
		return err
	}
	if decision.Type == approvalmodel.DecisionApproved || decision.Type == approvalmodel.DecisionApprovedForScope {
		return nil
	}
	return fmt.Errorf("search_skill_resources未通过审批: %s", decision.Reason)
}
