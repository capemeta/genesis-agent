package run_skill_command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	InputObserver  InputObserver
	InputResolver  InputResolver
	InputStager    workcontract.InputStager
	Finalizer      artifactcontract.RequiredDeliverableFinalizer
}

// InputResolver 是产品控制面的裸路径到已授权 ResourceRef 转换端口。
type InputResolver interface {
	ResolveInputs(ctx context.Context, inputs []string) ([]workmodel.ResourceRef, error)
}

// InputObserver 在成功执行后记录本 Run 已审批输入的交付上下文。
type InputObserver interface {
	RecordInputs(ctx context.Context, inputs []string)
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
inputs 可选，语义是控制面 stage 源：支持工作区相对路径（如 foo.py、data.csv）、$WORK_DIR/...、或用户在本次任务中指定的宿主机绝对文件路径；禁止 /workspace/... 等执行面绝对路径。若提供，会在执行审批后复制到当前 Skill 工作目录，供命令用相对文件名访问；禁止用 run_command / Copy-Item 手动搬运输入文件。文件已在工作目录或仅跑包内脚本时请省略。
command 写相对文件名或包内 scripts/...；禁止把物理路径写进 command。正确：inputs=["foo.py"] + command="python foo.py"。
返回 metadata.execution_backend / degraded（勿把物理路径写入 inputs/command）。
produced 只返回 Harness 生成的不透明 candidate_id/name 投影，不含路径或 locator。用户可见交付文件名以你在 skill 工作目录中写出的产物文件名为准（produced.name）；Harness 按该名 Publish/Delivery，不会从用户自然语言抠文件名。required 交付物唯一匹配时 Harness 自动发布交付；多候选时只允许调用 select_deliverable_candidate。不要用 write_file 伪造 .pptx/.docx/.xlsx/.pdf。
`),
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"skill":      {Type: "string", Description: "Skill 名称，例如 office-ppt"},
				"command":    {Type: "string", Description: "在 Skill 工作目录执行的命令，例如 python create_pdfs.py；避免多行 python -c / node -e"},
				"inputs":     {Type: "array", Description: "可选控制面 stage 源：工作区相对路径（如 foo.py）、$WORK_DIR/foo 或用户指定的宿主机绝对文件；禁止 /workspace/...。运行时会在审批后 stage 到 Skill 工作目录", Items: &tool.ParameterSchema{Type: "string"}},
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
		// 同一 Run 内相同 Skill 主体稳定复用 session workspace：第三方 Skill 的相对 cwd、
		// staged inputs、中间状态与显式 produced 发布必须跨多条命令保持一致。
		// 生命周期仍受当前 Run/session 控制，不等于跨 Run 的长期持久化。
		Intent:          workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeSession},
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
	in.Inputs = autoDetectReferencedInputs(command, in.Inputs, prepared.Execution.Workspace)
	var resolvedSources []workmodel.ResourceRef
	var controlPlaneStagingMS int64
	if len(in.Inputs) > 0 {
		if t.deps.InputResolver == nil || t.deps.InputStager == nil {
			return "", fmt.Errorf("INPUT_PERMISSION_DENIED: 当前产品未配置受控 InputRef staging")
		}
		stagingStarted := time.Now()
		resolvedInputs, err := expandControlPlaneInputs(in.Inputs, prepared.Execution.Workspace)
		if err != nil {
			return "", workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("解析 run_skill_command inputs: %w", err))
		}
		resolvedSources, err = t.deps.InputResolver.ResolveInputs(ctx, resolvedInputs)
		if err != nil {
			return "", err
		}
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
	if shouldHarnessFallback(t.deps.Sandbox, backend, result) {
		fallbackBackend := execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Provider: "local-host", Authority: "host"}
		fallbackExecution, prepareErr := control.PrepareExecution(ctx, workcontract.PrepareExecutionRequest{
			Subject:         execmodel.ExecutionSubjectRef{TaskID: "skill:" + skill},
			Backend:         fallbackBackend,
			Intent:          workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeSession},
			RequestedAccess: execmodel.WorkspaceAccessReadWrite,
		})
		if prepareErr != nil {
			return "", fmt.Errorf("准备 sandbox 降级 execution: %w", prepareErr)
		}
		fallbackInputs := workmodel.InputManifest{}
		if len(resolvedSources) > 0 {
			stagingStarted := time.Now()
			fallbackInputs, err = t.deps.InputStager.Stage(ctx, workcontract.StageRequest{Binding: fallbackExecution.Binding, Sources: resolvedSources})
			controlPlaneStagingMS += time.Since(stagingStarted).Milliseconds()
			if err != nil {
				return "", err
			}
		}
		fallbackSandbox := cloneSandbox(t.deps.Sandbox)
		fallbackSandbox.Mode = execmodel.SandboxDisabled
		fallbackSandbox.Provider = ""
		result, err = t.deps.Runner.Run(ctx, scriptcontract.RunRequest{
			Catalog: t.deps.CatalogRequest, Skill: skill, Command: command, Inputs: fallbackInputs,
			Binding: fallbackExecution.Binding, Backend: fallbackExecution.Backend, StateRoot: manifestState.StateRoot, ProjectDir: manifestState.ProjectDir,
			TimeoutMS: in.TimeoutMS, Sandbox: fallbackSandbox,
		})
		if err != nil {
			return "", err
		}
		if result != nil {
			result.Warnings = append([]string{"sandbox optional 已由 Harness 创建独立 Host execution attempt 后降级"}, result.Warnings...)
		}
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
	if result != nil && result.OK && t.deps.InputObserver != nil {
		t.deps.InputObserver.RecordInputs(ctx, in.Inputs)
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
	for _, raw := range inputs {
		expanded, ok, err := workmodel.ExpandLogicalPath(raw, workspace)
		if err != nil {
			return nil, err
		}
		if ok {
			resolved = append(resolved, expanded)
			continue
		}
		resolved = append(resolved, raw)
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

func shouldHarnessFallback(profile execmodel.SandboxProfile, backend execmodel.ExecutionBackendRef, result *scriptcontract.RunResult) bool {
	return profile.Mode == execmodel.SandboxOptional && backend.Kind == execmodel.BackendKindRemote && result != nil && !result.OK && result.FailureKind == "sandbox_unavailable"
}

var referencedInputFilePattern = regexp.MustCompile(`(?i)"([^"]+\.(?:pptx|docx|xlsx|pdf|csv|json|txt|md|py|js|png|jpg|jpeg))"|'([^']+\.(?:pptx|docx|xlsx|pdf|csv|json|txt|md|py|js|png|jpg|jpeg))'|(?:^|\s)([A-Za-z0-9_\p{Han}./-]+\.(?:pptx|docx|xlsx|pdf|csv|json|txt|md|py|js|png|jpg|jpeg))(?:$|\s)`)

func autoDetectReferencedInputs(command string, inputs []string, workspace execmodel.ExecutionWorkspace) []string {
	seen := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		seen[strings.TrimSpace(in)] = true
	}

	matches := referencedInputFilePattern.FindAllStringSubmatch(command, -1)
	for _, m := range matches {
		var candidate string
		for _, group := range m[1:] {
			if group != "" {
				candidate = strings.TrimSpace(group)
				break
			}
		}
		if candidate == "" || seen[candidate] {
			continue
		}
		if strings.HasPrefix(candidate, "scripts/") || candidate == "main.py" || candidate == "app.py" {
			continue
		}
		expanded, ok, err := workmodel.ExpandLogicalPath(candidate, workspace)
		if err != nil || !ok {
			continue
		}
		st, err := os.Stat(expanded)
		if err == nil && !st.IsDir() {
			inputs = append(inputs, candidate)
			seen[candidate] = true
		}
	}
	return inputs
}
