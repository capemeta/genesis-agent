// Package read_skill_resource 实现 Skill 包内资源读取工具。
package read_skill_resource

import (
	"context"
	"encoding/json"
	"fmt"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"path"
	"strings"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	capcontract "genesis-agent/internal/capabilities/capability/contract"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

type Deps struct {
	Service interface {
		Resolve(context.Context, skillcontract.ResolveRequest) (model.ResolvedInvocation, error)
		ReadResource(context.Context, skillcontract.ResourceRequest) (model.ResourceContent, error)
		ReadBoundResource(context.Context, skillcontract.BoundResourceRequest) (model.ResourceContent, error)
	}
	Approval       approvalcontract.Service
	CatalogRequest skillcontract.CatalogRequest
	Registry       capcontract.Registry
}

type Tool struct{ deps Deps }

type input struct {
	Skill    string `json:"skill,omitempty"`
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
	return &tool.Info{
		Name: "read_skill_resource",
		Description: "读取已加载或已发现 Skill 包内 references、scripts、assets 的 UTF-8 文本资源。" +
			"resource 可为完整 ResourceID（如 office-ppt/design.md），也可在提供 skill/package 时使用短名（如 design.md、references/guide.md），运行时会按包自动限定。" +
			"二进制 assets 只能先通过 list_skill_resources 发现，不能用本工具直接读取。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"skill":     {Type: "string", Description: "Skill 名称或 qualified_name，例如 office-ppt"},
				"package":   {Type: "string", Description: "可选 package id，用于直接定位 skill"},
				"resource":  {Type: "string", Description: "ResourceID 或短名：office-ppt/design.md 或 design.md"},
				"max_bytes": {Type: "integer", Description: "最大读取字节数"},
			},
			Required: []string{"resource"},
		},
		Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: true, ConcurrencySafe: true, NeedsPermission: true},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolparam.Decode(params, &in); err != nil {
		return "", fmt.Errorf("解析read_skill_resource参数失败: %w", err)
	}
	name := strings.TrimSpace(in.Skill)
	pkg := model.PackageID(strings.TrimSpace(in.Package))
	rawResource := strings.TrimSpace(in.Resource)
	binding, bound := skillcontract.InvocationBindingFromContext(ctx)
	if bound {
		if err := skillcontract.ValidateBoundTarget(binding, name, pkg); err != nil {
			return "", err
		}
		name = binding.Handle
		pkg = binding.Package.PackageID
	}
	if name == "" && pkg == "" {
		name = skillNameFromQualifiedResource(rawResource)
	}
	qualifier := name
	if bound {
		qualifier = binding.PhysicalSkill
	}
	resource := model.QualifySkillResource(string(pkg), qualifier, rawResource)
	if resource == "" {
		return "", fmt.Errorf("resource不能为空")
	}
	if !bound && name != "" {
		if resolved, resolveErr := t.deps.Service.Resolve(ctx, skillcontract.ResolveRequest{CatalogRequest: t.deps.CatalogRequest, Name: name}); resolveErr == nil && resolved.Definition.AgentMode.Mode == model.AgentModeFork {
			if prepared, ok := workcontract.PreparedRunFromContext(ctx); ok && prepared.Manifest.ParentRunID == "" {
				return "", fmt.Errorf("FORBIDDEN_FORK_SKILL_EXECUTION: Invocation %q声明为fork，父Agent禁止读取其包内资源；请调用Skill(skill=%q, task=...)委派子Run", resolved.Definition.Handle, resolved.Definition.Handle)
			}
		}
	}
	if err := t.authorize(ctx, pkg, resource); err != nil {
		return "", err
	}
	if err := t.ensureIndexed(ctx, pkg, name, resource); err != nil {
		return "", err
	}
	var content model.ResourceContent
	var err error
	if bound {
		content, err = t.deps.Service.ReadBoundResource(ctx, skillcontract.BoundResourceRequest{Binding: binding, Resource: resource, MaxBytes: in.MaxBytes})
	} else {
		content, err = t.deps.Service.ReadResource(ctx, skillcontract.ResourceRequest{
			ResolveRequest: skillcontract.ResolveRequest{CatalogRequest: t.deps.CatalogRequest, Name: name},
			PackageID:      pkg,
			Resource:       resource,
			MaxBytes:       in.MaxBytes,
		})
	}
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(output{SkillQualifiedName: content.Skill.Name, Resource: string(content.Resource), Content: content.Content, Truncated: content.Truncated})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// skillNameFromQualifiedResource 从完整 ResourceID 推导资源所有者。
// 仅接受至少两段的包内路径，短名仍必须显式提供 skill/package。
func skillNameFromQualifiedResource(resource string) string {
	normalized := strings.Trim(strings.TrimSpace(strings.ReplaceAll(resource, `\`, "/")), "/")
	if normalized == "" || !strings.Contains(normalized, "/") {
		return ""
	}
	return strings.SplitN(normalized, "/", 2)[0]
}

func (t *Tool) ensureIndexed(ctx context.Context, pkg model.PackageID, name string, resource model.ResourceID) error {
	if t.deps.Registry == nil {
		return nil
	}
	records, err := t.deps.Registry.ListCapabilities(ctx, capmodel.CapabilityQuery{Types: []capmodel.CapabilityType{capmodel.CapabilityTypeSkillResource}})
	if err != nil {
		return err
	}
	needles := map[string]struct{}{}
	for _, c := range model.ResourceLookupCandidates(string(pkg), name, string(resource)) {
		needles[normalizeResourcePath(c)] = struct{}{}
	}
	matchedScope := false
	for _, record := range records {
		if !matchesPackageOrSkill(record, pkg, name) {
			continue
		}
		matchedScope = true
		rp := normalizeResourcePath(record.ResourcePath)
		if _, ok := needles[rp]; ok {
			return nil
		}
		if _, ok := needles[path.Base(rp)]; ok && (string(pkg) == "" || strings.HasPrefix(rp, string(pkg)+"/") || strings.Contains(strings.ToLower(record.Name), strings.ToLower(name))) {
			return nil
		}
	}
	if !matchedScope {
		return nil
	}
	return fmt.Errorf("skill resource %q 未在CapabilityIndex中启用或不存在", resource)
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
