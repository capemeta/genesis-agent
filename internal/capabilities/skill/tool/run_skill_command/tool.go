package run_skill_command

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// Deps 是 run_skill_command 工具依赖。
type Deps struct {
	Runner         scriptcontract.Runner
	CatalogRequest skillcontract.CatalogRequest
	Sandbox        execmodel.SandboxProfile
	InputResolver  InputResolver
	InputStager    workcontract.InputStager
	Finalizer      artifactcontract.RequiredDeliverableFinalizer
}

// InputResolver 是产品控制面的裸路径到已授权 ResourceRef 转换端口。
type InputResolver interface {
	ResolveInputs(ctx context.Context, inputs []string) ([]workmodel.ResourceRef, error)
	// ResolveAvailableInputs 只跳过当前 execution 根内不存在的候选；越界、权限和版本错误仍必须失败。
	ResolveAvailableInputs(ctx context.Context, inputs []string) ([]workmodel.ResourceRef, error)
}

type Tool struct {
	deps Deps
}

type input struct {
	Skill     string   `json:"skill"`
	Command   string   `json:"command"`
	Inputs    []string `json:"inputs,omitempty"`
	TimeoutMS int64    `json:"timeout_ms,omitempty"`
}

func New(deps Deps) (tool.Tool, error) {
	if deps.Runner == nil {
		return nil, fmt.Errorf("skill command runner未配置")
	}
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name: "run_skill_command",
		Description: strings.TrimSpace(`
在当前 Skill 的持久工作目录中按原文执行命令。
当第三方 SKILL.md 写 python scripts/foo.py、python -m bar、node scripts/foo.js、pdftoppm ... 时，应直接把整条命令放进 command，由运行时负责 materialize skill、准备工作目录、注入环境并选择合适的 sandbox/profile。
需要执行 JS/Python 时，默认先写入脚本再执行 python foo.py / node foo.js；禁止 python -c / node -e 多行或长串内联（Windows/远程 shell 引号易失败）。仅极短单行探测可例外。
Harness 会自动把当前 Run 已绑定输入和 command 引用的入口脚本 stage 到 Skill 工作目录。inputs 仅用于补充当前工作区内、command 未直接引用的相对路径；禁止宿主机绝对路径、/workspace 路径和跨根猜测。禁止用 run_command / Copy-Item 手动搬运输入文件。
command 写相对文件名或包内 scripts/...；禁止把物理路径写进 command。正确：inputs=["foo.py"] + command="python foo.py"。
返回 metadata.execution_backend / degraded（勿把物理路径写入 inputs/command）。
produced 只返回 Harness 生成的不透明 candidate_id/name 投影，不含路径或 locator。用户可见交付文件名以你在 skill 工作目录中写出的产物文件名为准（produced.name）；Harness 按该名 Publish/Delivery，不会从用户自然语言抠文件名。required 交付物唯一匹配时 Harness 自动发布交付；多候选时只允许调用 select_deliverable_candidate。不要用 write_file 伪造 .pptx/.docx/.xlsx/.pdf。
`),
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"skill":      {Type: "string", Description: "Skill 名称，例如 office-ppt"},
				"command":    {Type: "string", Description: "在 Skill 工作目录执行的命令，例如 python create_pdfs.py；避免多行 python -c / node -e"},
				"inputs":     {Type: "array", Description: "可选补充输入，只接受当前 Run 根内相对路径；已绑定输入和 command 入口脚本会自动 stage", Items: &tool.ParameterSchema{Type: "string"}},
				"timeout_ms": {Type: "integer", Description: "超时毫秒，默认 120000"},
			},
			Required: []string{"skill", "command"},
		},
		Traits: tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false, NeedsPermission: true},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	harnessStarted := time.Now()
	var in input
	if err := toolparam.Decode(params, &in); err != nil {
		return "", fmt.Errorf("解析run_skill_command参数失败: %w", err)
	}
	skill := strings.TrimSpace(in.Skill)
	command := strings.TrimSpace(in.Command)
	if skill == "" || command == "" {
		return "", fmt.Errorf("skill与command不能为空")
	}
	control, ok := workcontract.ControlPlaneFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("EXECUTION_BINDING_REQUIRED: run_skill_command 缺少 workspace control plane")
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("EXECUTION_BINDING_REQUIRED: run_skill_command 缺少调用方 execution workspace")
	}
	backend := trustedExecutionBackend(t.deps.Sandbox)
	execution, err := control.PrepareExecution(ctx, workcontract.PrepareExecutionRequest{
		Subject: execmodel.ExecutionSubjectRef{TaskID: "skill:" + skill},
		Backend: backend,
		// 同一 Run 内相同 Skill 主体稳定复用 task workspace：第三方 Skill 的相对 cwd、
		// staged inputs和中间状态可跨多条命令保持一致，但不会错误升级成跨 Run session 生命周期。
		Intent:          workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask},
		RequestedAccess: execmodel.WorkspaceAccessReadWrite,
	})
	if err != nil {
		return "", fmt.Errorf("准备 Skill execution: %w", err)
	}
	binding := execution.Binding
	manifestState, err := control.GetRunManifest(ctx, binding.Owner.TenantID, binding.Owner.RunID)
	if err != nil {
		return "", err
	}
	manifest := workmodel.InputManifest{}
	requiredInputs, optionalEntries := collectWorkspaceInputs(command, in.Inputs, prepared.Manifest.View)
	var resolvedSources []workmodel.ResourceRef
	var controlPlaneStagingMS int64
	if len(requiredInputs) > 0 || len(optionalEntries) > 0 {
		if t.deps.InputResolver == nil || t.deps.InputStager == nil {
			if len(requiredInputs) > 0 {
				return "", fmt.Errorf("INPUT_PERMISSION_DENIED: 当前产品未配置受控 InputRef staging")
			}
			// 命令入口可能属于不可变 Skill 包；没有输入控制面时交给 materializer 做最终判定。
			optionalEntries = nil
		}
	}
	if len(requiredInputs) > 0 || len(optionalEntries) > 0 {
		stagingStarted := time.Now()
		resolvedInputs, err := expandControlPlaneInputs(requiredInputs, prepared.Execution.Workspace)
		if err != nil {
			return "", workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("解析 run_skill_command inputs: %w", err))
		}
		resolvedSources, err = t.deps.InputResolver.ResolveInputs(ctx, resolvedInputs)
		if err != nil {
			return "", err
		}
		optionalInputs, err := expandControlPlaneInputs(optionalEntries, prepared.Execution.Workspace)
		if err != nil {
			return "", workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("解析 run_skill_command command entry: %w", err))
		}
		available, err := t.deps.InputResolver.ResolveAvailableInputs(ctx, optionalInputs)
		if err != nil {
			return "", err
		}
		resolvedSources = mergeInputSources(resolvedSources, available)
		manifest, err = t.deps.InputStager.Stage(ctx, workcontract.StageRequest{Binding: binding, Sources: resolvedSources})
		if err != nil {
			return "", err
		}
		controlPlaneStagingMS = time.Since(stagingStarted).Milliseconds()
	}
	result, err := t.deps.Runner.Run(ctx, scriptcontract.RunRequest{
		Catalog:    t.deps.CatalogRequest,
		Skill:      skill,
		Command:    command,
		Inputs:     manifest,
		Binding:    binding,
		Backend:    execution.Backend,
		StateRoot:  manifestState.StateRoot,
		ProjectDir: manifestState.ProjectDir,
		TimeoutMS:  in.TimeoutMS,
		Sandbox:    cloneSandbox(t.deps.Sandbox),
	})
	if err != nil {
		return "", err
	}
	if result != nil {
		result.StagingDurationMS += controlPlaneStagingMS
		result.DurationMS = time.Since(harnessStarted).Milliseconds()
	}
	if result != nil && result.OK && t.deps.Finalizer != nil {
		finalized, finalizeErr := t.deps.Finalizer.FinalizeRequired(ctx, binding.Owner.TenantID, binding.Owner.RunID)
		if finalizeErr != nil {
			return "", fmt.Errorf("自动完成 required deliverable: %w", finalizeErr)
		}
		applyFinalization(result, finalized)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	if result != nil && !result.OK {
		msg := strings.TrimSpace(result.Error)
		if msg == "" {
			msg = "run_skill_command failed"
		}
		return string(data), fmt.Errorf("%s", msg)
	}
	return string(data), nil
}

func applyFinalization(result *scriptcontract.RunResult, finalized artifactmodel.FinalizationResult) {
	if result == nil {
		return
	}
	for _, resolution := range finalized.Resolutions {
		ids := append([]string(nil), resolution.CandidateIDs...)
		if resolution.SelectedID != "" {
			ids = append(ids, resolution.SelectedID)
		}
		for i := range result.Produced {
			for _, id := range ids {
				if result.Produced[i].CandidateID == id {
					result.Produced[i].DeliverableID = resolution.DeliverableID
				}
			}
		}
		switch resolution.Status {
		case "selection_required":
			result.OK = false
			result.FailureKind = "deliverable_selection_ambiguous"
			result.Error = fmt.Sprintf("DELIVERABLE_SELECTION_AMBIGUOUS: deliverable %s 有多个有效候选", resolution.DeliverableID)
			result.Warnings = append(result.Warnings, fmt.Sprintf("deliverable %s 有多个有效候选；仅可调用 select_deliverable_candidate 并提交 deliverable_id 与 candidate_id", resolution.DeliverableID))
		case "missing":
			result.OK = false
			result.FailureKind = "required_deliverable_not_produced"
			result.Error = fmt.Sprintf("DELIVERABLE_NOT_PRODUCED: required deliverable %s 尚无匹配候选", resolution.DeliverableID)
			result.Warnings = append(result.Warnings, fmt.Sprintf("required deliverable %s 尚无匹配候选", resolution.DeliverableID))
		case "delivered":
			result.Warnings = append(result.Warnings, fmt.Sprintf("deliverable %s 已由 Harness 发布并交付", resolution.DeliverableID))
			if warning := strings.TrimSpace(resolution.Warning); warning != "" {
				result.Warnings = append(result.Warnings, warning)
			}
		case "delivery_conflict":
			// 产物/QA 与交付解耦：冲突不把本命令打成失败，避免毒化后续 run_skill_command。
			warning := strings.TrimSpace(resolution.Warning)
			if warning == "" {
				warning = fmt.Sprintf("DELIVERY_TARGET_CONFLICT: deliverable %s 交付目标冲突", resolution.DeliverableID)
			}
			result.Warnings = append(result.Warnings, warning)
		}
	}
}

// expandControlPlaneInputs 只在可信控制面展开逻辑目录；模型仍只看到稳定的
// $WORK_DIR/... 等引用，物理路径不会进入 Tool schema、模型上下文或跨 backend 协议。
// 这里使用 Run 当前 execution，而不是派生 Skill execution：write_file("$WORK_DIR/...")
// 写入的是调用方工作区，随后再作为 InputRef stage 到隔离的 Skill 工作区。
func expandControlPlaneInputs(inputs []string, workspace execmodel.ExecutionWorkspace) ([]string, error) {
	resolved := make([]string, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, raw := range inputs {
		expanded, ok, err := workmodel.ExpandLogicalPath(raw, workspace)
		if err != nil {
			return nil, err
		}
		value := raw
		if ok {
			value = expanded
		}
		key := strings.ToLower(filepath.Clean(value))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		resolved = append(resolved, value)
	}
	return resolved, nil
}

func cloneSandbox(in execmodel.SandboxProfile) execmodel.SandboxProfile {
	out := in
	if in.Metadata != nil {
		out.Metadata = make(map[string]string, len(in.Metadata))
		for k, v := range in.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}

// trustedExecutionBackend 只解析产品 bootstrap 注入的 SandboxProfile，不读取模型参数。
func trustedExecutionBackend(profile execmodel.SandboxProfile) execmodel.ExecutionBackendRef {
	if profile.Mode == "" || profile.Mode == execmodel.SandboxDisabled {
		return execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Provider: "local-host", Authority: "host"}
	}
	provider := strings.ToLower(strings.TrimSpace(profile.Provider))
	switch provider {
	case "", "local", "local_host", "host":
		return execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindLocalSandbox, Provider: "local-platform", Authority: "host"}
	default:
		return execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindRemote, Provider: strings.TrimSpace(profile.Provider), Authority: "remote-executor"}
	}
}

var referencedInputFilePattern = regexp.MustCompile(`(?i)"([^"]+\.(?:pptx|docx|xlsx|pdf|csv|json|txt|md|py|js|png|jpg|jpeg))"|'([^']+\.(?:pptx|docx|xlsx|pdf|csv|json|txt|md|py|js|png|jpg|jpeg))'|(?:^|\s)([A-Za-z0-9_\p{Han}./-]+\.(?:pptx|docx|xlsx|pdf|csv|json|txt|md|py|js|png|jpg|jpeg))(?:$|\s)`)

func collectWorkspaceInputs(command string, explicit []string, view workmodel.WorkspaceViewManifest) ([]string, []string) {
	required := make([]string, 0, len(view.Entries)+len(explicit))
	optional := make([]string, 0, 1)
	seen := make(map[string]struct{}, cap(required)+1)
	add := func(value string) {
		value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
		key := strings.ToLower(value)
		if value == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		required = append(required, value)
	}
	for _, entry := range view.Entries {
		add(string(entry.Path))
	}
	for _, value := range explicit {
		add(value)
	}
	for _, match := range referencedInputFilePattern.FindAllStringSubmatch(command, -1) {
		candidate := ""
		for _, group := range match[1:] {
			if strings.TrimSpace(group) != "" {
				candidate = strings.TrimSpace(group)
				break
			}
		}
		normalized := strings.TrimPrefix(strings.ReplaceAll(candidate, `\`, "/"), "./")
		if normalized == "" || strings.HasPrefix(strings.ToLower(normalized), "scripts/") || normalized == "main.py" || normalized == "app.py" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			optional = append(optional, normalized)
		}
		break
	}
	return required, optional
}

func mergeInputSources(left, right []workmodel.ResourceRef) []workmodel.ResourceRef {
	result := append([]workmodel.ResourceRef(nil), left...)
	seen := make(map[string]struct{}, len(left)+len(right))
	for _, ref := range left {
		seen[ref.Authority+"\x00"+ref.Scheme+"\x00"+ref.ID+"\x00"+ref.Version] = struct{}{}
	}
	for _, ref := range right {
		key := ref.Authority + "\x00" + ref.Scheme + "\x00" + ref.ID + "\x00" + ref.Version
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, ref)
	}
	return result
}
