// Package read_skill_resource 实现 Skill 包内资源读取工具。
package read_skill_resource

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
	Name     string `json:"name,omitempty"`
	Package  string `json:"package,omitempty"`
	Resource string `json:"resource"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

type output struct {
	SkillQualifiedName string `json:"skill_qualified_name"`
	Resource           string `json:"resource"`
	Content            string `json:"content"`
	Truncated          bool   `json:"truncated"`
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
	return &tool.Info{Name: "read_skill_resource", Description: "读取已加载或已发现 Skill 包内 references/assets/scripts 资源。resource 是不透明ID，不能当本地路径使用。", Parameters: &tool.ParameterSchema{Type: "object", Properties: map[string]*tool.ParameterSchema{"name": {Type: "string", Description: "Skill 名称或 qualified_name"}, "package": {Type: "string", Description: "可选 package id，用于直接定位 skill"}, "resource": {Type: "string", Description: "Skill ResourceID，例如 review/references/guide.md"}, "max_bytes": {Type: "integer", Description: "最大读取字节数"}}, Required: []string{"resource"}}, Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: true, ConcurrencySafe: true, NeedsPermission: true}}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := json.Unmarshal([]byte(params), &in); err != nil {
		return "", fmt.Errorf("解析read_skill_resource参数失败: %w", err)
	}
	resource := model.ResourceID(strings.TrimSpace(in.Resource))
	if resource == "" {
		return "", fmt.Errorf("resource不能为空")
	}
	pkg := model.PackageID(strings.TrimSpace(in.Package))
	if err := t.authorize(ctx, pkg, resource); err != nil {
		return "", err
	}
	content, err := t.deps.Service.ReadResource(ctx, skillcontract.ResourceRequest{ResolveRequest: skillcontract.ResolveRequest{CatalogRequest: t.deps.CatalogRequest, Name: strings.TrimSpace(in.Name)}, PackageID: pkg, Resource: resource, MaxBytes: in.MaxBytes})
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(output{SkillQualifiedName: content.Skill.QualifiedName, Resource: string(content.Resource), Content: content.Content, Truncated: content.Truncated})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *Tool) authorize(ctx context.Context, pkg model.PackageID, resource model.ResourceID) error {
	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{ToolName: "read_skill_resource", Action: approvalmodel.ActionSkillResourceRead, Resource: approvalmodel.Resource{Type: "skill_resource", URI: string(pkg) + ":" + string(resource), Display: string(resource), Metadata: map[string]string{"trusted": "true", "package": string(pkg), "resource": string(resource)}}, Reason: "读取Skill包内资源", Risk: approvalmodel.RiskLow})
	if err != nil {
		return err
	}
	if decision.Type == approvalmodel.DecisionApproved || decision.Type == approvalmodel.DecisionApprovedForScope {
		return nil
	}
	return fmt.Errorf("read_skill_resource未通过审批: %s", decision.Reason)
}
