// Package skill 实现 Skill 网关工具。
package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
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
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	"genesis-agent/internal/runtime/progress"
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
	inFlight sync.Map
}

// SetForkTask 由共享 bootstrap 在 Task 创建后注入唯一委派网关。
// Skill 不直接依赖 Controller 或具体运行策略；fork 优先走 Delegator。
func (t *Tool) SetForkTask(task tool.Tool) { t.forkTask = task }

type input struct {
	Skill      string   `json:"skill,omitempty"`
	Resource   string   `json:"resource,omitempty"`
	Args       string   `json:"args,omitempty"`
	InputFiles []string `json:"inputs,omitempty"`
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
		Name:        toolNameSkill,
		Description: staticDescription,
		DescriptionFunc: t.renderDescription,
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"skill":    {Type: "string", Description: "Skill 名称或 qualified_name，必须来自 <available_skills>，例如 office-ppt"},
				"resource": {Type: "string", Description: "可选，不透明 resource id，用于消除同名冲突"},
				"args":     {Type: "string", Description: "传给 Skill 的任务具体要求或上下文描述。对于 context: fork 技能（如 office-ppt），必须在此填入详细的任务目标与要求，禁止为空。"},
				"inputs":   {Type: "array", Items: &tool.ParameterSchema{Type: "string"}, Description: "可选，显式指定引用的输入文件列表（如 [\"ultra5-comparison-summary.md\"]）。若未填则由系统按工作区真实文件自动匹配。"},
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
	if err := t.checkRequiredCapabilities(ctx, meta); err != nil {
		return "", err
	}
	if defaultContext(meta.Context) == model.ContextModeFork {
		return t.fork(ctx, meta, resolveReq, in.Args, in.InputFiles)
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

func (t *Tool) fork(ctx context.Context, meta model.Metadata, resolveReq skillcontract.ResolveRequest, args string, explicitInputs []string) (string, error) {
	// 瞬间向 TUI 推送子 Agent 派生进度
	progress.Emit(ctx, progress.Event{
		Kind:      progress.KindSubAgent,
		Phase:     progress.PhaseStart,
		Component: "skill-gateway",
		Name:      meta.QualifiedName,
		Summary:   fmt.Sprintf("[Sub-Agent Worker] 派生沙箱 Worker 子智能体独立执行技能: %s", meta.QualifiedName),
	})

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
	fingerprint := fmt.Sprintf("%s:%s", meta.QualifiedName, args)
	if _, loaded := t.inFlight.LoadOrStore(fingerprint, true); loaded {
		return fmt.Sprintf("【重复任务拦截】相同的技能任务 [%s] 正在后台运行中，请勿重复发起。请等待当前任务交卷。", meta.QualifiedName), nil
	}
	defer t.inFlight.Delete(fingerprint)

	// 方案 B：若大模型显式传了 inputs，直接优先使用；未传则回退到方案 A (工作区真实存在性交集匹配)
	inputFiles := normalizeInputs(explicitInputs)
	if len(inputFiles) == 0 {
		inputFiles = extractWorkspaceInputFiles(ctx, args)
	}

	// 规则校验：对于 context: fork 隔离技能，必须在 args 中有明确描述，或者在 inputs 中有显式文件。禁止同时为空！
	if args == "" && len(inputFiles) == 0 {
		return "", fmt.Errorf("MISSING_INPUT_REQUIRED: Skill %q 声明为 context: fork 物理沙箱隔离技能，委派子 Agent 必须在 args 中提供具体的任务描述，或在 inputs 中指定输入文件。请补充参数重新调用，例如 Skill(skill=%q, args=\"根据文件内容制作对比PPT\", inputs=[\"ultra5-comparison-summary.md\"])", meta.QualifiedName, meta.QualifiedName)
	}

	agentType := strings.TrimSpace(meta.Agent)
	req := subagentcontract.DelegateRequest{
		Description:        "执行 Skill " + meta.QualifiedName,
		AllowedTools:       cloneStrings(meta.AllowedTools),
		InputFiles:         inputFiles,
		PromptOrigin:       "skill_fork",
		SkillQAPolicy:      strings.TrimSpace(meta.QA.Policy),
		SkillQAEnforcement: strings.TrimSpace(meta.QA.Enforcement),
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
		req.Prompt = args
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

// checkRequiredCapabilities 仅处理 enforcement=required 的能力门槛。
// vision：依据 Run 的 EffectiveVisionMode（由主模型 / router.vision 的真实 supports_image 求解），
// degraded_text 时拒绝加载；optional 或不配置不做任何限制。
func (t *Tool) checkRequiredCapabilities(ctx context.Context, meta model.Metadata) error {
	for _, req := range meta.Requires {
		if !model.IsRequiredEnforcement(req.Enforcement) {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(req.Kind))
		switch kind {
		case "vision":
			mode, ok := viewimage.VisionModeFromContext(ctx)
			if !ok {
				mode = vision.ModeDegradedText
			}
			if !vision.HasImageCapability(mode) {
				return fmt.Errorf("SKILL_CAPABILITY_REQUIRED: Skill %q 声明 requires.vision=required，但当前 Run 无可用视觉模型（主模型与 router.vision 均未配置 supports_image=true）。请配置 models.*.supports_image 和/或 router.vision 后再调用", meta.QualifiedName)
			}
		default:
			return fmt.Errorf("SKILL_CAPABILITY_REQUIRED: Skill %q 声明了未知能力门槛 kind=%q", meta.QualifiedName, req.Kind)
		}
	}
	return nil
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

func normalizeInputs(inputs []string) []string {
	if len(inputs) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var res []string
	for _, item := range inputs {
		clean := strings.TrimSpace(item)
		if clean != "" && !seen[clean] {
			seen[clean] = true
			res = append(res, clean)
		}
	}
	return res
}

func extractWorkspaceInputFiles(ctx context.Context, text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	seen := make(map[string]bool)
	var res []string

	addCandidate := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && !seen[candidate] {
			seen[candidate] = true
			res = append(res, candidate)
		}
	}

	// 1. 方案 A 核心：工作区真实存在性交集匹配（Workspace File Intersection Matching）
	// 不依赖正则猜中文词汇边界，而是拿真实工作区现存的文件名/别名与 Prompt 字符串比对（strings.Contains）
	if prepared, ok := workcontract.PreparedRunFromContext(ctx); ok {
		for _, inputRef := range prepared.Manifest.Inputs.Inputs {
			name := strings.TrimSpace(inputRef.Name)
			alias := strings.TrimSpace(string(inputRef.Alias))
			if name != "" && strings.Contains(text, name) {
				addCandidate(name)
			} else if alias != "" && strings.Contains(text, alias) {
				addCandidate(alias)
			}
		}
		dirsToCheck := []string{
			prepared.Execution.Workspace.WorkDir,
			prepared.Execution.Workspace.InputDir,
			prepared.Manifest.ProjectDir,
		}
		for _, dir := range dirsToCheck {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				continue
			}
			if entries, err := os.ReadDir(dir); err == nil {
				for _, entry := range entries {
					if entry.IsDir() {
						continue
					}
					name := entry.Name()
					if name != "" && strings.Contains(text, name) {
						addCandidate(name)
					}
				}
			}
		}
	}

	// 2. 方案 A 备用：纯 ASCII/绝对路径/无 PreparedRun 时的标准模式提取
	for _, m := range extractFallbackInputFiles(text) {
		addCandidate(m)
	}

	return res
}

var fallbackPathPattern = regexp.MustCompile(`(?i)(?:[a-zA-Z]:[\\/][^\s"'，。；：、（）()]+|[a-zA-Z0-9_\-\.]+\.(?:md|markdown|txt|csv|tsv|json|ya?ml|pdf|docx?|xlsx?|pptx?|html?|xml|go|py|js|ts|java|sql|png|jpe?g))`)

func extractFallbackInputFiles(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	matches := fallbackPathPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var res []string
	for _, m := range matches {
		clean := strings.TrimSpace(m)
		if clean != "" && !seen[clean] {
			seen[clean] = true
			res = append(res, clean)
		}
	}
	return res
}
