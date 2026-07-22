// Package skill 实现固定 Skill Invocation 网关。
package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	"genesis-agent/internal/capabilities/llm/vision"
	viewimage "genesis-agent/internal/capabilities/media/tool/view_image"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	subagentcontract "genesis-agent/internal/capabilities/subagent/contract"
	subagentmodel "genesis-agent/internal/capabilities/subagent/model"
	subagentprompt "genesis-agent/internal/capabilities/subagent/prompt"
	tool "genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/progress"
)

const (
	toolNameSkill     = "Skill"
	staticDescription = "调用已发现的Skill Invocation。skill必须来自本工具description中的<available_skills>；task和inputs必须显式提供，禁止把技能名当作独立工具调用。"
)

type InputResolver interface {
	ResolveInputs(ctx context.Context, inputs []string) ([]workmodel.ResourceRef, error)
}

type Deps struct {
	Service                skillcontract.Service
	Approval               approvalcontract.Service
	CatalogRequest         skillcontract.CatalogRequest
	EnabledTools           []string
	InputResolver          InputResolver
	RunInitializer         artifactcontract.RunInitializer
	LocalSandboxAvailable  bool
	RemoteSandboxAvailable bool
	PolicyVersion          string
}

type Tool struct {
	deps     Deps
	forkTask tool.Tool
	inFlight sync.Map
}

func (t *Tool) SetForkTask(task tool.Tool) { t.forkTask = task }

type input struct {
	Skill  string   `json:"skill"`
	Task   string   `json:"task,omitempty"`
	Inputs []string `json:"inputs,omitempty"`
}

type dependencyOutput struct {
	Type        string `json:"type"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
}

type output struct {
	Type          string             `json:"type"`
	Name          string             `json:"name"`
	QualifiedName string             `json:"qualified_name"`
	PhysicalSkill string             `json:"physical_skill"`
	InvocationID  string             `json:"invocation_id"`
	BindingID     string             `json:"binding_id"`
	Resource      string             `json:"resource"`
	Content       string             `json:"content,omitempty"`
	Task          string             `json:"task,omitempty"`
	Truncated     bool               `json:"truncated"`
	AllowedTools  []string           `json:"allowed_tools"`
	Context       string             `json:"context"`
	Dependencies  []dependencyOutput `json:"dependencies,omitempty"`
}

func New(deps Deps) (tool.Tool, error) {
	if deps.Service == nil || deps.Approval == nil {
		return nil, fmt.Errorf("skill service与approval service不能为空")
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name: toolNameSkill, Description: staticDescription, DescriptionFunc: t.renderDescription,
		Parameters: &tool.ParameterSchema{Type: "object", Properties: map[string]*tool.ParameterSchema{
			"skill":  {Type: "string", Description: "Invocation handle，必须来自<available_skills>，例如office-ppt-read或office-ppt"},
			"task":   {Type: "string", Description: "任务目标；是否必填由Invocation契约决定"},
			"inputs": {Type: "array", Items: &tool.ParameterSchema{Type: "string"}, Description: "显式输入资源引用或当前Run内已授权别名；数量和类型由Invocation契约校验"},
		}, Required: []string{"skill"}},
		Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false, NeedsPermission: true, RequiresUserInteraction: true},
	}
}

func (t *Tool) renderDescription(ctx context.Context) (string, error) {
	catalog, err := t.deps.Service.RenderAvailableSkills(ctx, t.deps.CatalogRequest)
	if err != nil {
		return staticDescription, err
	}
	return staticDescription + "\n\n<skills_instructions>\n只调用一次Skill(skill, task?, inputs?)完成Invocation选择；不要传entrypoint、sandbox、model、qa或deliverable参数。\n</skills_instructions>\n\n" + strings.TrimSpace(catalog), nil
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolparam.Decode(params, &in); err != nil {
		return "", fmt.Errorf("解析Skill参数失败（仅支持skill/task/inputs）: %w", err)
	}
	return t.load(ctx, in, true, "model")
}

func (t *Tool) LoadExplicitSkill(ctx context.Context, req skillcontract.ExplicitLoadRequest) (string, error) {
	return t.load(ctx, input{Skill: req.Skill, Task: req.Task, Inputs: req.Inputs}, false, "explicit")
}

// GetInvocationBinding 供 Run Runtime 使用 opaque binding_id 激活 inline Invocation。
// 模型不应也不需要传递 binding_id。
func (t *Tool) GetInvocationBinding(ctx context.Context, bindingID string) (model.InvocationBinding, error) {
	runID, _ := contextutil.GetRunID(ctx)
	tenantID, _ := contextutil.GetTenantID(ctx)
	binding, err := t.deps.Service.GetBinding(ctx, skillcontract.BindingLookup{ID: strings.TrimSpace(bindingID), TenantID: tenantID, RunID: runID})
	if err != nil {
		return model.InvocationBinding{}, err
	}
	if binding.ID != strings.TrimSpace(bindingID) || binding.RunID != runID || (binding.TenantID != "" && binding.TenantID != tenantID) {
		return model.InvocationBinding{}, fmt.Errorf("SKILL_INVOCATION_ACTIVATION_FAILED: binding不属于当前Run")
	}
	if binding.AgentMode.Mode != model.AgentModeMain {
		return model.InvocationBinding{}, fmt.Errorf("SKILL_INVOCATION_ACTIVATION_FAILED: fork binding不能在父Run内激活")
	}
	return binding, nil
}

func (t *Tool) load(ctx context.Context, in input, modelCall bool, invocationSource string) (string, error) {
	handle := strings.TrimSpace(in.Skill)
	if handle == "" {
		return "", fmt.Errorf("SKILL_REQUEST_INVALID: skill不能为空")
	}
	resolved, err := t.deps.Service.Resolve(ctx, skillcontract.ResolveRequest{CatalogRequest: t.deps.CatalogRequest, Name: handle, ModelCall: modelCall, Invocation: invocationSource})
	if err != nil {
		return "", err
	}
	if err := t.checkRecursion(ctx, resolved.CatalogItem.ID); err != nil {
		return "", err
	}
	if err := t.dispatchPreHook(ctx, modelCall, invocationSource, resolved); err != nil {
		return "", err
	}

	capabilities := t.resolveTargetCapabilities(ctx)
	if err := checkRequiredCapabilities(resolved.Definition, capabilities); err != nil {
		return "", err
	}
	toolPolicy, err := resolveToolPolicy(t.deps.EnabledTools, resolved.Definition.ToolPolicy)
	if err != nil {
		return "", err
	}
	executionPolicy, err := t.resolveExecutionPolicy(resolved)
	if err != nil {
		return "", err
	}
	inputRefs, err := t.resolveInputs(ctx, in.Inputs)
	if err != nil {
		return "", err
	}
	depReport, err := t.checkDependencies(ctx, resolved, invocationSource)
	if err != nil {
		return "", err
	}
	if err := t.authorize(ctx, resolved, depReport, invocationSource); err != nil {
		return "", err
	}

	runID, _ := contextutil.GetRunID(ctx)
	tenantID, _ := contextutil.GetTenantID(ctx)
	parentRunID := ""
	if resolved.Definition.AgentMode.Mode == model.AgentModeFork {
		parentRunID = runID
	}
	binding, err := t.deps.Service.CreateBinding(ctx, skillcontract.BindingRequest{
		Resolved: resolved, TenantID: tenantID, RunID: runID, ParentRunID: parentRunID,
		Task: in.Task, Inputs: inputRefs, ToolPolicy: toolPolicy, ExecutionPolicy: executionPolicy,
		Capabilities: capabilities, PolicySnapshotVersion: t.deps.PolicyVersion,
	})
	if err != nil {
		return "", err
	}
	if resolved.Definition.AgentMode.Mode == model.AgentModeFork {
		return t.fork(ctx, resolved, binding, depReport)
	}
	if err := t.initializeDeliverables(ctx, binding); err != nil {
		return "", err
	}
	injection, err := t.deps.Service.Load(ctx, skillcontract.LoadRequest{Resolved: resolved, Binding: binding})
	if err != nil {
		return "", err
	}
	if err := t.dispatchPostHook(ctx, modelCall, invocationSource, resolved); err != nil {
		return "", err
	}
	return toJSON(output{
		Type: "skill_injection", Name: binding.Handle, QualifiedName: binding.Handle, PhysicalSkill: binding.PhysicalSkill,
		InvocationID: binding.InvocationID, BindingID: binding.ID, Resource: string(injection.Resource), Content: injection.Contents,
		Task: binding.Task, AllowedTools: cloneStrings(binding.ToolPolicy.Allowed), Context: string(binding.AgentMode.Mode), Dependencies: depReport.Outputs,
	})
}

func (t *Tool) resolveInputs(ctx context.Context, inputs []string) ([]workmodel.ResourceRef, error) {
	inputs = normalizeStrings(inputs)
	if len(inputs) == 0 {
		return nil, nil
	}
	if t.deps.InputResolver == nil {
		return nil, fmt.Errorf("SKILL_REQUEST_INVALID: 当前产品未配置ResourceRef输入解析器")
	}
	refs, err := t.deps.InputResolver.ResolveInputs(ctx, inputs)
	if err != nil {
		return nil, fmt.Errorf("SKILL_REQUEST_INVALID: 解析显式inputs: %w", err)
	}
	if len(refs) != len(inputs) {
		return nil, fmt.Errorf("SKILL_REQUEST_INVALID: inputs解析数量不一致")
	}
	return refs, nil
}

func resolveToolPolicy(base []string, declared model.ToolPolicy) (model.EffectiveToolPolicy, error) {
	base = normalizeStrings(base)
	allow := normalizeStrings(declared.Allow)
	required := normalizeStrings(declared.Required)
	if len(base) == 0 || len(allow) == 0 {
		return model.EffectiveToolPolicy{}, fmt.Errorf("SKILL_TOOL_POLICY_UNSATISFIABLE: base或allow工具集为空")
	}
	baseSet := stringSet(base)
	allowed := make([]string, 0, len(allow))
	for _, name := range allow {
		if _, ok := baseSet[name]; ok {
			allowed = append(allowed, name)
		}
	}
	if len(allowed) == 0 {
		return model.EffectiveToolPolicy{}, fmt.Errorf("SKILL_TOOL_POLICY_UNSATISFIABLE: Tool Policy求交为空")
	}
	allowedSet := stringSet(allowed)
	for _, name := range required {
		if _, ok := allowedSet[name]; !ok {
			return model.EffectiveToolPolicy{}, fmt.Errorf("SKILL_TOOL_POLICY_UNSATISFIABLE: 缺少required Tool %q", name)
		}
	}
	return model.EffectiveToolPolicy{Base: base, Allowed: allowed, Required: required}, nil
}

func (t *Tool) resolveExecutionPolicy(resolved model.ResolvedInvocation) (model.EffectiveExecutionPolicy, error) {
	backends := append([]string(nil), resolved.Profile.Sandbox.Backends...)
	if len(backends) == 0 {
		if resolved.Profile.Sandbox.Required {
			backends = []string{"remote_sandbox", "local_platform_sandbox"}
		} else {
			backends = []string{"remote_sandbox", "local_platform_sandbox", "local_host"}
		}
	}

	selected := ""
	for _, b := range backends {
		switch b {
		case "remote_sandbox":
			if t.deps.RemoteSandboxAvailable {
				selected = "remote_sandbox"
			}
		case "local_platform_sandbox":
			if t.deps.LocalSandboxAvailable {
				selected = "local_platform_sandbox"
			}
		case "local_host":
			selected = "local_host"
		}
		if selected != "" {
			break
		}
	}

	if selected == "" {
		return model.EffectiveExecutionPolicy{}, fmt.Errorf("SKILL_RUNTIME_PROFILE_UNAVAILABLE: Invocation %q 所配置的物理后端列表 %v 在当前环境中均不可用", resolved.Definition.Handle, backends)
	}

	preferred := backends[0]
	degraded := selected != preferred
	var warnings []string
	if degraded {
		warnings = append(warnings, fmt.Sprintf("Invocation %q 物理后端由 %s 自动降级至 %s", resolved.Definition.Handle, preferred, selected))
	}

	hasHost := false
	for _, b := range backends {
		if b == "local_host" {
			hasHost = true
			break
		}
	}

	return model.EffectiveExecutionPolicy{
		SandboxRequired:  !hasHost,
		ExecutionMode:    resolved.Profile.Sandbox.ExecutionMode,
		Backends:         backends,
		SelectedBackend:  selected,
		PreferredBackend: preferred,
		RequestedBackend: preferred,
		AllowDegradation: len(backends) > 1,
		Degraded:         degraded,
		Warnings:         warnings,
	}, nil
}

func (t *Tool) resolveTargetCapabilities(ctx context.Context) model.EffectiveCapabilitySnapshot {
	mode, ok := viewimage.VisionModeFromContext(ctx)
	if !ok {
		mode = vision.ModeDegradedText
	}
	// Skill fork 当前复用同一受信 Agent/模型装配，仅隔离 Run 和工具/工作区；因此这里的
	// EffectiveVisionMode 正是即将启动的子执行者能力，而不是按模型名猜测。
	return model.EffectiveCapabilitySnapshot{VisionMode: string(mode), CheckedAt: time.Now().UTC()}
}

func checkRequiredCapabilities(definition model.InvocationDefinition, snapshot model.EffectiveCapabilitySnapshot) error {
	for _, requirement := range definition.Requires {
		if !model.IsRequiredEnforcement(requirement.Enforcement) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(requirement.Kind)) {
		case "vision":
			if !vision.HasImageCapability(vision.Mode(snapshot.VisionMode)) {
				return fmt.Errorf("SKILL_CAPABILITY_REQUIRED: Invocation %q要求真实视觉能力，目标执行者vision_mode=%s", definition.Handle, snapshot.VisionMode)
			}
		default:
			return fmt.Errorf("SKILL_CAPABILITY_REQUIRED: Invocation %q要求未知能力%q", definition.Handle, requirement.Kind)
		}
	}
	return nil
}

func (t *Tool) fork(ctx context.Context, resolved model.ResolvedInvocation, binding model.InvocationBinding, deps dependencyReport) (string, error) {
	progress.Emit(ctx, progress.Event{Kind: progress.KindSubAgent, Phase: progress.PhaseStart, Component: "skill-gateway", Name: binding.Handle, Summary: "派生隔离子Run执行Invocation: " + binding.Handle})
	if t.forkTask == nil {
		return "", fmt.Errorf("SKILL_RUNTIME_PROFILE_UNAVAILABLE: Invocation %q要求fork，但Task网关未注入", binding.Handle)
	}
	delegator, ok := t.forkTask.(subagentcontract.Delegator)
	if !ok {
		return "", fmt.Errorf("SKILL_RUNTIME_PROFILE_UNAVAILABLE: Task网关未实现Delegator")
	}
	injection, err := t.deps.Service.Load(ctx, skillcontract.LoadRequest{Resolved: resolved, Binding: binding})
	if err != nil {
		return "", err
	}
	fingerprint := binding.IdempotencyKey
	if _, loaded := t.inFlight.LoadOrStore(fingerprint, struct{}{}); loaded {
		return "", fmt.Errorf("SKILL_INVOCATION_IN_PROGRESS: 相同Invocation任务正在执行")
	}
	defer t.inFlight.Delete(fingerprint)

	allowedTools := appendSubAgentTasklistTools(binding.ToolPolicy.Allowed)
	definition := &subagentmodel.Definition{
		Name: subagentprompt.SkillForkDefinitionName(binding.Handle), Description: "Skill Invocation fork: " + binding.Handle,
		WhenToUse: "仅由Skill网关按Runtime Manifest创建", SystemPrompt: "", Tools: cloneStrings(allowedTools),
		MaxTurns: binding.AgentMode.MaxTurns, MaxTokens: binding.AgentMode.MaxTokens, MaxToolCalls: binding.AgentMode.MaxToolCalls,
		TimeoutSec: binding.AgentMode.TimeoutSec,
	}
	req := subagentcontract.DelegateRequest{
		Prompt: injection.Contents, Description: "执行Skill Invocation " + binding.Handle,
		AllowedTools: cloneStrings(allowedTools), Definition: definition,
		SnapshotMode: subagentcontract.SnapshotModeSkillIsolated, PromptOrigin: "skill_fork",
		MaxTurns: binding.AgentMode.MaxTurns, MaxTokens: binding.AgentMode.MaxTokens,
		MaxToolCalls: binding.AgentMode.MaxToolCalls, TimeoutSec: binding.AgentMode.TimeoutSec,
		InvocationBinding: binding, Deliverables: declaredDeliverables(binding.Result),
	}
	for _, input := range binding.Inputs {
		req.InputRefs = append(req.InputRefs, input.Ref)
	}
	_ = deps
	return delegator.Delegate(skillcontract.WithInvocationAncestors(ctx, append(skillcontract.InvocationAncestors(ctx), resolved.CatalogItem.ID)), req)
}

func (t *Tool) initializeDeliverables(ctx context.Context, binding model.InvocationBinding) error {
	deliverables := declaredDeliverables(binding.Result)
	if len(deliverables) == 0 {
		return nil
	}
	if t.deps.RunInitializer == nil {
		return fmt.Errorf("SKILL_DELIVERABLE_REQUIRED: Invocation声明交付物，但RunInitializer未配置")
	}
	return t.deps.RunInitializer.InitializeRun(ctx, artifactcontract.RunInitializationRequest{TenantID: binding.TenantID, RunID: binding.RunID, Deliverables: deliverables})
}

func declaredDeliverables(result model.ResultContract) []artifactcontract.DeclaredDeliverable {
	if result.Kind != model.ResultKindDeliverables {
		return nil
	}
	out := make([]artifactcontract.DeclaredDeliverable, 0, len(result.Deliverables))
	for _, declaration := range result.Deliverables {
		out = append(out, artifactcontract.DeclaredDeliverable{
			ID: declaration.ID, Required: declaration.Required, Cardinality: declaration.Cardinality, Role: declaration.Role, DesiredName: declaration.DesiredName,
			AcceptedMIMEs: cloneStrings(declaration.AcceptedMIMEs), AcceptedSuffix: cloneStrings(declaration.AcceptedSuffixes),
			QAPolicy: declaration.QA.Policy, QAEnforcement: declaration.QA.Enforcement, DeliveryPolicy: declaration.DeliveryPolicy,
		})
	}
	return out
}

type dependencyReport struct {
	Outputs          []dependencyOutput
	ExternalCount    int
	RequiresApproval bool
	DependencyCount  int
}

func (t *Tool) checkDependencies(ctx context.Context, resolved model.ResolvedInvocation, invocationSource string) (dependencyReport, error) {
	report := dependencyReport{}
	available := stringSet(normalizeStrings(t.deps.EnabledTools))
	for _, dependency := range resolved.Profile.Dependencies.Tools {
		kind := strings.ToLower(strings.TrimSpace(dependency.Type))
		value := strings.TrimSpace(dependency.Value)
		if kind == "" || value == "" {
			continue
		}
		report.DependencyCount++
		item := dependencyOutput{Type: kind, Value: value, Description: dependency.Description, Status: "available"}
		if kind == "tool" {
			if _, ok := available[value]; !ok {
				return report, fmt.Errorf("SKILL_RUNTIME_PROFILE_UNAVAILABLE: Invocation %q依赖未启用Tool %q", resolved.Definition.Handle, value)
			}
		} else {
			item.Status = "requires_approval"
			report.ExternalCount++
			report.RequiresApproval = true
		}
		report.Outputs = append(report.Outputs, item)
	}
	if report.RequiresApproval {
		decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{
			ToolName: toolNameSkill, Action: approvalmodel.ActionSkillLoad,
			Resource: approvalmodel.Resource{Type: "skill.dependencies", URI: model.SkillDependenciesDecisionKey(resolved.Definition.Handle), Display: resolved.Definition.Handle},
			Reason:   "Skill Invocation声明外部依赖", Risk: approvalmodel.RiskMedium,
			SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce, approvalmodel.GrantScopeSession},
			Metadata:        map[string]string{"invocation_source": invocationSource, "dependency_count": fmt.Sprintf("%d", report.DependencyCount)},
		})
		if err != nil {
			return report, err
		}
		if decision.Type != approvalmodel.DecisionApproved && decision.Type != approvalmodel.DecisionApprovedForScope {
			return report, fmt.Errorf("Skill依赖未通过审批: %s", firstNonEmpty(decision.Reason, string(decision.Type)))
		}
	}
	return report, nil
}

func (t *Tool) authorize(ctx context.Context, resolved model.ResolvedInvocation, deps dependencyReport, source string) error {
	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{
		ToolName: toolNameSkill, Action: approvalmodel.ActionSkillLoad,
		Resource: approvalmodel.Resource{Type: "skill_invocation", URI: model.SkillDecisionKey(resolved.Definition.Handle), Display: resolved.Definition.Handle,
			Metadata: map[string]string{"authority": resolved.CatalogItem.Authority.String(), "package": string(resolved.CatalogItem.PackageID), "package_digest": resolved.CatalogItem.PackageDigest, "invocation_id": resolved.Definition.ID, "source": source, "dependency_count": fmt.Sprintf("%d", deps.DependencyCount)}},
		Reason: "调用Skill Invocation", Risk: approvalmodel.RiskLow,
	})
	if err != nil {
		return err
	}
	if decision.Type != approvalmodel.DecisionApproved && decision.Type != approvalmodel.DecisionApprovedForScope {
		return fmt.Errorf("Skill未通过审批: %s", firstNonEmpty(decision.Reason, string(decision.Type)))
	}
	return nil
}

func (t *Tool) checkRecursion(ctx context.Context, invocationID string) error {
	ancestors := skillcontract.InvocationAncestors(ctx)
	if len(ancestors) >= 8 {
		return fmt.Errorf("SKILL_RECURSION_DENIED: Invocation调用深度超过8")
	}
	for _, ancestor := range ancestors {
		if ancestor == invocationID {
			return fmt.Errorf("SKILL_RECURSION_DENIED: Invocation祖先链形成环: %s", invocationID)
		}
	}
	return nil
}

func (t *Tool) dispatchPreHook(ctx context.Context, modelCall bool, source string, resolved model.ResolvedInvocation) error {
	if !modelCall {
		return nil
	}
	if dispatcher := hookcontract.FromContext(ctx); dispatcher != nil {
		result, err := dispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventPreSkillUse, MatchKey: resolved.Definition.Handle, Payload: map[string]any{"skill_handle": resolved.Definition.Handle, "invocation_id": resolved.Definition.ID, "source": source}})
		if err != nil {
			return fmt.Errorf("执行PreSkillUse Hook失败: %w", err)
		}
		hookcontract.AppendAdditionalContext(ctx, result.AdditionalContext...)
		if result.Blocked {
			return fmt.Errorf("Skill Invocation %q被Hook阻断: %s", resolved.Definition.Handle, result.BlockReason)
		}
	}
	return nil
}

func (t *Tool) dispatchPostHook(ctx context.Context, modelCall bool, source string, resolved model.ResolvedInvocation) error {
	if !modelCall {
		return nil
	}
	if dispatcher := hookcontract.FromContext(ctx); dispatcher != nil {
		result, err := dispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventPostSkillUse, MatchKey: resolved.Definition.Handle, Payload: map[string]any{"skill_handle": resolved.Definition.Handle, "invocation_id": resolved.Definition.ID, "source": source, "injected": true}})
		if err != nil {
			return fmt.Errorf("执行PostSkillUse Hook失败: %w", err)
		}
		hookcontract.AppendAdditionalContext(ctx, result.AdditionalContext...)
	}
	return nil
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}
func cloneStrings(values []string) []string { return append([]string(nil), values...) }
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func toJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func appendSubAgentTasklistTools(allowed []string) []string {
	out := cloneStrings(allowed)
	set := stringSet(out)
	for _, toolName := range []string{"todo_read", "todo_update_step", "todo_write"} {
		if _, ok := set[toolName]; !ok {
			out = append(out, toolName)
			set[toolName] = struct{}{}
		}
	}
	return out
}

var _ tool.Tool = (*Tool)(nil)
