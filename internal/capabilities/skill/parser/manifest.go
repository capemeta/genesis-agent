package parser

import (
	"fmt"
	"mime"
	"path"
	"regexp"
	"sort"
	"strings"

	"genesis-agent/internal/capabilities/skill/model"
)

var (
	identifierPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	mimePattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*$`)
)

type ManifestValidationOptions struct {
	KnownTools        map[string]struct{}
	KnownCapabilities map[string]struct{}
	KnownQAPolicies   map[string]struct{}
}

// NormalizeSandboxRequirement 校验并归一化 SandboxRequirement 节点，补充默认 Backends 列表与边缘兜底。
func NormalizeSandboxRequirement(sb model.SandboxRequirement) model.SandboxRequirement {
	if sb.ExecutionMode == "" {
		sb.ExecutionMode = model.ExecutionModePerCall
	}
	if len(sb.Backends) == 0 {
		if sb.Required {
			sb.Backends = []string{"remote_sandbox", "local_platform_sandbox"}
		} else {
			sb.Backends = []string{"remote_sandbox", "local_platform_sandbox", "local_host"}
		}
	} else {
		var cleaned []string
		seen := make(map[string]struct{})
		for _, b := range sb.Backends {
			b = strings.TrimSpace(strings.ToLower(b))
			if b == "" {
				continue
			}
			if _, exists := seen[b]; !exists {
				seen[b] = struct{}{}
				cleaned = append(cleaned, b)
			}
		}
		sb.Backends = cleaned
	}
	hasHost := false
	for _, b := range sb.Backends {
		if b == "local_host" {
			hasHost = true
			break
		}
	}
	sb.Required = !hasHost
	return sb
}

func (p *Parser) ParseRuntimeManifest(data []byte, skillName string) (model.RuntimeManifest, error) {
	if len(data) == 0 {
		return model.RuntimeManifest{}, fmt.Errorf("%s为空", model.RuntimeManifestFileName)
	}
	if len(data) > model.MaxManifestBytes {
		return model.RuntimeManifest{}, fmt.Errorf("%s超过%d字节", model.RuntimeManifestFileName, model.MaxManifestBytes)
	}
	var manifest model.RuntimeManifest
	if err := decodeStrictYAML(data, &manifest); err != nil {
		return model.RuntimeManifest{}, fmt.Errorf("SKILL_MANIFEST_INVALID: %w", err)
	}
	if manifest.RuntimeProfiles != nil {
		for k, profile := range manifest.RuntimeProfiles {
			profile.Sandbox = NormalizeSandboxRequirement(profile.Sandbox)
			manifest.RuntimeProfiles[k] = profile
		}
	}
	if manifest.Invocations != nil {
		for i, inv := range manifest.Invocations {
			if inv.AgentMode.Mode == "inline" {
				inv.AgentMode.Mode = model.AgentModeMain
			}
			manifest.Invocations[i] = inv
		}
	}
	if err := ValidateRuntimeManifest(manifest, skillName, ManifestValidationOptions{}); err != nil {
		return model.RuntimeManifest{}, err
	}
	return manifest, nil
}

func ValidateRuntimeManifest(m model.RuntimeManifest, skillName string, opts ManifestValidationOptions) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("SKILL_MANIFEST_INVALID: "+format, args...)
	}
	if strings.TrimSpace(m.Schema) != model.RuntimeManifestSchemaV1 {
		return fail("schema必须为%s", model.RuntimeManifestSchemaV1)
	}
	if m.Skill != strings.TrimSpace(skillName) || m.Skill == "" {
		return fail("skill %q 必须等于SKILL.md.name %q", m.Skill, skillName)
	}
	if len(m.RuntimeProfiles) == 0 || len(m.RuntimeProfiles) > model.MaxRuntimeProfiles {
		return fail("runtime_profiles数量必须在1到%d之间", model.MaxRuntimeProfiles)
	}
	if len(m.Invocations) == 0 || len(m.Invocations) > model.MaxInvocations {
		return fail("invocations数量必须在1到%d之间", model.MaxInvocations)
	}
	profileIDs := sortedMapKeys(m.RuntimeProfiles)
	for _, id := range profileIDs {
		if !identifierPattern.MatchString(id) {
			return fail("runtime profile id非法: %q", id)
		}
		if err := validateRuntimeProfile(id, m.RuntimeProfiles[id]); err != nil {
			return fail("runtime profile %q: %v", id, err)
		}
	}
	ids := make(map[string]struct{}, len(m.Invocations))
	handles := make(map[string]struct{}, len(m.Invocations))
	for i, invocation := range m.Invocations {
		if err := validateInvocation(invocation, m.RuntimeProfiles, opts); err != nil {
			return fail("invocations[%d]: %v", i, err)
		}
		if _, ok := ids[invocation.ID]; ok {
			return fail("invocation id重复: %q", invocation.ID)
		}
		ids[invocation.ID] = struct{}{}
		if _, ok := handles[invocation.Handle]; ok {
			return fail("invocation handle重复: %q", invocation.Handle)
		}
		handles[invocation.Handle] = struct{}{}
	}
	return nil
}

func validateRuntimeProfile(id string, profile model.RuntimeProfile) error {
	sb := NormalizeSandboxRequirement(profile.Sandbox)
	if sb.ExecutionMode != model.ExecutionModePerCall && sb.ExecutionMode != model.ExecutionModeSandboxedSession {
		return fmt.Errorf("sandbox.execution_mode非法: %q", sb.ExecutionMode)
	}
	if len(sb.Backends) == 0 {
		return fmt.Errorf("sandbox.backends不能为空")
	}
	for _, b := range sb.Backends {
		switch b {
		case "remote_sandbox", "local_platform_sandbox", "local_host":
		default:
			return fmt.Errorf("sandbox.backends 包含未知后端 %q，必须为 remote_sandbox / local_platform_sandbox / local_host", b)
		}
	}
	deps := profile.Dependencies
	count := len(deps.Tools) + len(deps.Runtime.Python) + len(deps.Runtime.Node) + len(deps.Runtime.System)
	if count > model.MaxDependenciesPerProfile {
		return fmt.Errorf("dependencies超过%d项", model.MaxDependenciesPerProfile)
	}
	seen := map[string]struct{}{}
	for _, dep := range deps.Tools {
		kind := strings.ToLower(strings.TrimSpace(dep.Type))
		value := strings.TrimSpace(dep.Value)
		if kind == "" || value == "" {
			return fmt.Errorf("tool dependency的type/value不能为空")
		}
		key := kind + "\x00" + value
		if _, ok := seen[key]; ok {
			return fmt.Errorf("dependency重复: %s/%s", kind, value)
		}
		seen[key] = struct{}{}
	}
	for manager, packages := range map[string][]model.RuntimePackage{
		"python": deps.Runtime.Python, "node": deps.Runtime.Node, "system": deps.Runtime.System,
	} {
		for _, pkg := range packages {
			name := strings.TrimSpace(pkg.Name)
			if name == "" || strings.ContainsAny(name, "\x00\r\n") {
				return fmt.Errorf("%s runtime dependency name非法", manager)
			}
			key := manager + "\x00" + strings.ToLower(name)
			if _, ok := seen[key]; ok {
				return fmt.Errorf("runtime dependency重复: %s/%s", manager, name)
			}
			seen[key] = struct{}{}
		}
	}
	_ = id
	return nil
}

func validateInvocation(inv model.InvocationDefinition, profiles map[string]model.RuntimeProfile, opts ManifestValidationOptions) error {
	if !identifierPattern.MatchString(inv.ID) || !identifierPattern.MatchString(inv.Handle) {
		return fmt.Errorf("id/handle只允许小写字母、数字和连字符")
	}
	if strings.TrimSpace(inv.Description) == "" || len([]rune(inv.Description)) > model.MaxDescriptionLen {
		return fmt.Errorf("description为空或超过%d字符", model.MaxDescriptionLen)
	}
	mode := inv.AgentMode.Mode
	if mode == "inline" {
		mode = model.AgentModeMain
	}
	if mode != model.AgentModeMain && mode != model.AgentModeFork {
		return fmt.Errorf("agent_mode.mode非法: %q", mode)
	}
	if inv.AgentMode.TimeoutSec < 0 || inv.AgentMode.MaxTurns < 0 || inv.AgentMode.MaxTokens < 0 || inv.AgentMode.MaxToolCalls < 0 {
		return fmt.Errorf("agent_mode预算不能为负数")
	}
	if _, ok := profiles[inv.RuntimeProfile]; !ok {
		return fmt.Errorf("runtime_profile不存在: %q", inv.RuntimeProfile)
	}
	if inv.Request.Inputs.MinItems < 0 || inv.Request.Inputs.MaxItems < inv.Request.Inputs.MinItems || inv.Request.Inputs.MaxItems > model.MaxInputs {
		return fmt.Errorf("request.inputs min/max非法")
	}
	if inv.Request.Inputs.MaxItems > 0 && inv.Request.Inputs.Access != model.InputAccessReadOnly {
		return fmt.Errorf("request.inputs.access仅支持read_only")
	}
	if inv.Request.Inputs.MaxItems == 0 && inv.Request.Inputs.MinItems != 0 {
		return fmt.Errorf("request.inputs未允许输入却声明min_items")
	}
	if err := validateTypes(inv.Request.Inputs.AcceptedSuffixes, inv.Request.Inputs.AcceptedMIMEs); err != nil {
		return fmt.Errorf("request.inputs: %v", err)
	}
	if inv.Prompt.SkillBody != model.SkillBodyInclude && inv.Prompt.SkillBody != model.SkillBodyOmit {
		return fmt.Errorf("prompt.skill_body非法: %q", inv.Prompt.SkillBody)
	}
	if inv.Prompt.Instructions != "" {
		if err := validatePackageRelativePath(inv.Prompt.Instructions); err != nil {
			return fmt.Errorf("prompt.instructions: %v", err)
		}
		if !strings.HasSuffix(strings.ToLower(inv.Prompt.Instructions), ".md") {
			return fmt.Errorf("prompt.instructions必须是.md资源")
		}
	}
	if len(inv.ToolPolicy.Allow) == 0 || len(inv.ToolPolicy.Allow) > model.MaxToolsPerInvocation {
		return fmt.Errorf("tool_policy.allow必须在1到%d项之间", model.MaxToolsPerInvocation)
	}
	allow, err := validateNameSet(inv.ToolPolicy.Allow, "tool_policy.allow")
	if err != nil {
		return err
	}
	required, err := validateNameSet(inv.ToolPolicy.Required, "tool_policy.required")
	if err != nil {
		return err
	}
	for name := range required {
		if _, ok := allow[name]; !ok {
			return fmt.Errorf("tool_policy.required %q不在allow中", name)
		}
	}
	for name := range allow {
		if len(opts.KnownTools) > 0 {
			if _, ok := opts.KnownTools[name]; !ok {
				return fmt.Errorf("tool_policy声明未知Tool %q", name)
			}
		}
	}
	for _, req := range inv.Requires {
		if strings.TrimSpace(req.Kind) == "" || !validEnforcement(req.Enforcement) {
			return fmt.Errorf("requires声明非法")
		}
		if len(opts.KnownCapabilities) > 0 {
			if _, ok := opts.KnownCapabilities[req.Kind]; !ok {
				return fmt.Errorf("requires声明未知能力 %q", req.Kind)
			}
		}
	}
	if err := validateResult(inv.Result, opts); err != nil {
		return err
	}
	if requiresVisualQA(inv.Result) && !hasRequiredCapability(inv.Requires, "vision") {
		return fmt.Errorf("required visual QA必须同时声明requires: vision/required，避免启动后才发现无视觉能力")
	}
	return nil
}

func requiresVisualQA(result model.ResultContract) bool {
	for _, deliverable := range result.Deliverables {
		if deliverable.QA.Policy == "visual-qa/v1" && model.IsRequiredEnforcement(deliverable.QA.Enforcement) {
			return true
		}
	}
	return false
}

func hasRequiredCapability(requirements []model.CapabilityRequirement, kind string) bool {
	for _, requirement := range requirements {
		if requirement.Kind == kind && model.IsRequiredEnforcement(requirement.Enforcement) {
			return true
		}
	}
	return false
}

func validateResult(result model.ResultContract, opts ManifestValidationOptions) error {
	switch result.Kind {
	case model.ResultKindMessage:
		if len(result.Deliverables) != 0 {
			return fmt.Errorf("result.kind=message不得声明deliverables")
		}
		return nil
	case model.ResultKindDeliverables:
		if len(result.Deliverables) == 0 || len(result.Deliverables) > model.MaxDeliverables {
			return fmt.Errorf("result.kind=deliverables必须声明1到%d个deliverable", model.MaxDeliverables)
		}
	default:
		return fmt.Errorf("result.kind非法: %q", result.Kind)
	}
	ids := make(map[string]struct{}, len(result.Deliverables))
	for i, d := range result.Deliverables {
		if !identifierPattern.MatchString(d.ID) {
			return fmt.Errorf("deliverables[%d].id非法", i)
		}
		if _, ok := ids[d.ID]; ok {
			return fmt.Errorf("deliverable id重复: %q", d.ID)
		}
		ids[d.ID] = struct{}{}
		if d.Role != model.DeliverableRolePrimary && d.Role != model.DeliverableRoleSupporting {
			return fmt.Errorf("deliverable %q role非法", d.ID)
		}
		if d.Role == model.DeliverableRolePrimary && !d.Required {
			return fmt.Errorf("primary deliverable %q必须required", d.ID)
		}
		switch d.Cardinality {
		case model.DeliverableExactlyOne:
			if !d.Required {
				return fmt.Errorf("deliverable %q exactly_one必须required", d.ID)
			}
		case model.DeliverableZeroOrOne:
			if d.Required || d.Role == model.DeliverableRolePrimary {
				return fmt.Errorf("deliverable %q zero_or_one只能用于可选supporting", d.ID)
			}
		case model.DeliverableOneOrMore, model.DeliverableZeroOrMore:
			return fmt.Errorf("deliverable %q 当前控制面不支持多值cardinality %q", d.ID, d.Cardinality)
		default:
			return fmt.Errorf("deliverable %q cardinality非法", d.ID)
		}
		if d.DeliveryPolicy != model.DeliveryPolicyRunOutput {
			return fmt.Errorf("deliverable %q delivery_policy非法", d.ID)
		}
		if err := validateTypes(d.AcceptedSuffixes, d.AcceptedMIMEs); err != nil {
			return fmt.Errorf("deliverable %q: %v", d.ID, err)
		}
		if d.Required && strings.TrimSpace(d.DesiredName) == "" && len(d.AcceptedSuffixes) == 0 && len(d.AcceptedMIMEs) == 0 {
			return fmt.Errorf("required deliverable %q缺少可验证类型", d.ID)
		}
		if d.QA.Policy != "" {
			if !validEnforcement(d.QA.Enforcement) {
				return fmt.Errorf("deliverable %q qa.enforcement非法", d.ID)
			}
			if model.IsRequiredEnforcement(d.QA.Enforcement) && !d.Required {
				return fmt.Errorf("required QA必须绑定required deliverable %q", d.ID)
			}
			if len(opts.KnownQAPolicies) > 0 {
				if _, ok := opts.KnownQAPolicies[d.QA.Policy]; !ok {
					return fmt.Errorf("deliverable %q声明未知QA policy %q", d.ID, d.QA.Policy)
				}
			}
		} else if strings.TrimSpace(d.QA.Enforcement) != "" {
			return fmt.Errorf("deliverable %q缺少qa.policy", d.ID)
		}
	}
	return nil
}

func validateTypes(suffixes, mimes []string) error {
	seen := map[string]struct{}{}
	for _, suffix := range suffixes {
		if suffix != strings.ToLower(strings.TrimSpace(suffix)) || !strings.HasPrefix(suffix, ".") || len(suffix) < 2 || strings.ContainsAny(suffix, "/\\\x00") {
			return fmt.Errorf("accepted suffix非法: %q", suffix)
		}
		if _, ok := seen["s:"+suffix]; ok {
			return fmt.Errorf("accepted suffix重复: %q", suffix)
		}
		seen["s:"+suffix] = struct{}{}
	}
	for _, mediaType := range mimes {
		mediaType = strings.ToLower(strings.TrimSpace(mediaType))
		base, _, err := mime.ParseMediaType(mediaType)
		if err != nil || base != mediaType || !mimePattern.MatchString(mediaType) {
			return fmt.Errorf("accepted MIME非法: %q", mediaType)
		}
		if _, ok := seen["m:"+mediaType]; ok {
			return fmt.Errorf("accepted MIME重复: %q", mediaType)
		}
		seen["m:"+mediaType] = struct{}{}
	}
	return nil
}

func validatePackageRelativePath(value string) error {
	if value != strings.TrimSpace(value) || value == "" || strings.Contains(value, `\`) || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("路径必须是规范化包内相对路径")
	}
	clean := path.Clean(value)
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || hasWindowsVolume(clean) {
		return fmt.Errorf("路径越界或不是相对路径: %q", value)
	}
	return nil
}

func hasWindowsVolume(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) && value[1] == ':'
}

func validateNameSet(values []string, field string) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != strings.TrimSpace(value) || value == "" || strings.ContainsAny(value, "\x00\r\n") {
			return nil, fmt.Errorf("%s包含非法名称", field)
		}
		if _, ok := out[value]; ok {
			return nil, fmt.Errorf("%s包含重复项 %q", field, value)
		}
		out[value] = struct{}{}
	}
	return out, nil
}

func validEnforcement(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == string(model.EnforcementOptional) || value == string(model.EnforcementRequired)
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
