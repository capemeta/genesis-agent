// Package list_skill_resources 实现 Skill 包内资源清单工具。
package list_skill_resources

import (
	"context"
	"encoding/json"
	"fmt"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"strings"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	capcontract "genesis-agent/internal/capabilities/capability/contract"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	"genesis-agent/internal/capabilities/skill/script/scriptutil"
	tool "genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
)

type Deps struct {
	Service        skillcontract.Service
	Approval       approvalcontract.Service
	CatalogRequest skillcontract.CatalogRequest
	Registry       capcontract.Registry
}

type Tool struct{ deps Deps }

type input struct {
	Name    string `json:"name,omitempty"`
	Package string `json:"package,omitempty"`
}

type output struct {
	SkillQualifiedName string               `json:"skill_qualified_name"`
	Package            string               `json:"package"`
	Resources          []model.ResourceInfo `json:"resources"`
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
	return &tool.Info{Name: "list_skill_resources", Description: "列出 Skill 包内 references、scripts、assets 资源元数据。只返回 resource id、类型、文件名、大小和是否文本，不读取资源内容。", Parameters: &tool.ParameterSchema{Type: "object", Properties: map[string]*tool.ParameterSchema{"name": {Type: "string", Description: "Skill 名称或 qualified_name"}, "package": {Type: "string", Description: "可选 package id，用于直接定位 skill"}}}, Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: true, ConcurrencySafe: true, NeedsPermission: true}}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolparam.DecodeOptional(params, &in); err != nil {
		return "", fmt.Errorf("解析list_skill_resources参数失败: %w", err)
	}
	pkg := model.PackageID(strings.TrimSpace(in.Package))
	name := strings.TrimSpace(in.Name)
	if pkg == "" && name == "" {
		return "", fmt.Errorf("name或package必须提供一个")
	}
	if err := t.authorize(ctx, pkg, name); err != nil {
		return "", err
	}
	result, err := t.deps.Service.ListResources(ctx, skillcontract.ListResourcesRequest{ResolveRequest: skillcontract.ResolveRequest{CatalogRequest: t.deps.CatalogRequest, Name: name}, PackageID: pkg})
	if err != nil {
		return "", err
	}
	resources := t.filterIndexedResources(ctx, pkg, name, result.Resources)
	resources = filterExecutableScriptEntries(resources)
	data, err := json.Marshal(output{SkillQualifiedName: result.Skill.QualifiedName, Package: string(result.Skill.PackageID), Resources: resources})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *Tool) filterIndexedResources(ctx context.Context, pkg model.PackageID, name string, resources []model.ResourceInfo) []model.ResourceInfo {
	if t.deps.Registry == nil {
		return resources
	}
	records, err := t.deps.Registry.ListCapabilities(ctx, capmodel.CapabilityQuery{Types: []capmodel.CapabilityType{capmodel.CapabilityTypeSkillResource}})
	if err != nil {
		return resources
	}
	indexed := map[string]struct{}{}
	for _, record := range records {
		if !matchesPackageOrSkill(record, pkg, name) {
			continue
		}
		indexed[normalizeResourcePath(record.ResourcePath)] = struct{}{}
	}
	if len(indexed) == 0 {
		return resources
	}
	out := make([]model.ResourceInfo, 0, len(resources))
	for _, resource := range resources {
		if _, ok := indexed[normalizeResourcePath(string(resource.Resource))]; ok {
			out = append(out, resource)
		}
	}
	return out
}

// filterExecutableScriptEntries 对模型隐藏不可作为 run_skill_command 入口的辅助模块。
func filterExecutableScriptEntries(resources []model.ResourceInfo) []model.ResourceInfo {
	out := make([]model.ResourceInfo, 0, len(resources))
	for _, resource := range resources {
		if resource.Kind == model.ResourceKindScript && !scriptutil.IsExecutableScriptEntry(string(resource.Resource)) {
			continue
		}
		out = append(out, resource)
	}
	return out
}

func matchesPackageOrSkill(record capmodel.CapabilityIndexRecord, pkg model.PackageID, name string) bool {
	if pkg != "" {
		value := string(pkg)
		if record.Package != value && record.Spec != value {
			return false
		}
	}
	if strings.TrimSpace(name) == "" {
		return true
	}
	needle := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(strings.ToLower(record.Name), needle) || strings.Contains(strings.ToLower(record.ResourcePath), "/"+needle+"/")
}

func normalizeResourcePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	if idx := strings.Index(value, "skills/"); idx >= 0 {
		value = value[idx+len("skills/"):]
	}
	return strings.Trim(value, "/")
}
func (t *Tool) authorize(ctx context.Context, pkg model.PackageID, name string) error {
	display := firstNonEmpty(string(pkg), name)
	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{ToolName: "list_skill_resources", Action: approvalmodel.ActionSkillResourceRead, Resource: approvalmodel.Resource{Type: "skill_resource_list", URI: display + ":resources", Display: display, Metadata: map[string]string{"trusted": "true", "package": string(pkg), "name": name}}, Reason: "列出Skill包内资源", Risk: approvalmodel.RiskLow})
	if err != nil {
		return err
	}
	if decision.Type == approvalmodel.DecisionApproved || decision.Type == approvalmodel.DecisionApprovedForScope {
		return nil
	}
	return fmt.Errorf("list_skill_resources未通过审批: %s", decision.Reason)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
