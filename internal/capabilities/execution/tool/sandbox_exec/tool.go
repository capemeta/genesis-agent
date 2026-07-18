// Package sandbox_exec 实现模型可见的单一远程隔离执行入口。
package sandbox_exec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	execservice "genesis-agent/internal/capabilities/execution/service"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

const (
	defaultTimeout = 120 * time.Second
	maxTimeout     = 10 * time.Minute
)

// InputResolver 将已批准的项目相对路径冻结为版本化 ResourceRef。
type InputResolver interface {
	ResolveInputs(ctx context.Context, inputs []string) ([]workmodel.ResourceRef, error)
}

// Deps 是 sandbox_exec 工具依赖。
type Deps struct {
	Runner        execservice.SandboxCommandRunner
	InputResolver InputResolver
	InputStager   workcontract.InputStager
	Approval      approvalcontract.Service
	Finalizer     artifactcontract.RequiredDeliverableFinalizer
	Sandbox       execmodel.SandboxProfile
}

type input struct {
	Command   string   `json:"command"`
	Inputs    []string `json:"inputs,omitempty"`
	TimeoutMS int64    `json:"timeout_ms,omitempty"`
}

// Tool 是远程隔离命令工具；Session、上传、产物登记均由 Harness 隐式完成。
type Tool struct{ deps Deps }

// New 创建 sandbox_exec 工具。
func New(deps Deps) (toolcontract.Tool, error) {
	if deps.Runner == nil || deps.InputResolver == nil || deps.InputStager == nil || deps.Approval == nil {
		return nil, fmt.Errorf("sandbox_exec 缺少 runner/input resolver/input stager/approval")
	}
	if !strings.EqualFold(strings.TrimSpace(deps.Sandbox.Provider), "genesis-sandbox") {
		return nil, fmt.Errorf("sandbox_exec 仅接受受信 genesis-sandbox provider")
	}
	deps.Sandbox.Mode = execmodel.SandboxRequired
	if deps.Sandbox.RuntimeProfile == "" {
		deps.Sandbox.RuntimeProfile = execmodel.RuntimeProfileCodePolyglotBasic
	}
	deps.Sandbox.TaskType = execmodel.SandboxTaskShell
	deps.Sandbox.Operation = execmodel.SandboxOperationRunShell
	return &Tool{deps: deps}, nil
}

func (t *Tool) GetInfo() *toolcontract.Info {
	return &toolcontract.Info{
		Name:        "sandbox_exec",
		Description: "在远程隔离 sandbox 中执行命令。宿主项目命令使用 run_command；本工具的 inputs 仅接受当前项目内相对路径，Harness 会自动做权限校验、快照和上传。相同 Agent Session 会复用远程 WORK_DIR，中间文件可跨调用保留；需要交付的文件必须写入环境变量 OUTPUT_DIR 指向的目录，Harness 会自动登记产物。禁止传宿主绝对路径或 /workspace 路径。",
		Parameters: &toolcontract.ParameterSchema{
			Type: "object",
			Properties: map[string]*toolcontract.ParameterSchema{
				"command":    {Type: "string", Description: "在远程 WORK_DIR 执行的 Shell 命令"},
				"inputs":     {Type: "array", Description: "可选的宿主项目相对输入路径", Items: &toolcontract.ParameterSchema{Type: "string"}},
				"timeout_ms": {Type: "integer", Description: "超时毫秒，默认120000，最大600000"},
			},
			Required: []string{"command"},
		},
		Traits: toolcontract.ToolTraits{Exposure: toolcontract.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false, NeedsPermission: true},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolparam.Decode(params, &in); err != nil {
		return "", fmt.Errorf("解析sandbox_exec参数: %w", err)
	}
	command := strings.TrimSpace(in.Command)
	if command == "" {
		return "", fmt.Errorf("command不能为空")
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("EXECUTION_BINDING_REQUIRED: sandbox_exec 缺少 PreparedRun")
	}
	control, ok := workcontract.ControlPlaneFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("EXECUTION_BINDING_REQUIRED: sandbox_exec 缺少 workspace control plane")
	}
	profile := t.deps.Sandbox
	backend := execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindRemote, Provider: profile.Provider, Authority: "remote-executor"}
	execution, err := control.PrepareExecution(ctx, workcontract.PrepareExecutionRequest{
		Subject: execmodel.ExecutionSubjectRef{TaskID: "sandbox:command"}, Backend: backend,
		Intent:          workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeSession, BoundedInputs: true, BoundedOutputs: true, NeedsPersistentRun: true},
		RequestedAccess: execmodel.WorkspaceAccessReadWrite,
	})
	if err != nil {
		return "", fmt.Errorf("准备 sandbox execution: %w", err)
	}

	inputs := mergeInputs(prepared.Manifest.View, in.Inputs)
	decision, err := t.deps.Approval.Authorize(ctx, approvalmodel.Request{
		ToolName: "sandbox_exec", Action: approvalmodel.ActionCommandExec,
		Resource:        approvalmodel.Resource{Type: "sandbox_command", URI: "sandbox-command://" + execution.Binding.ID + "/" + command, Display: command},
		Reason:          "在远程隔离环境执行命令；批准后自动 stage 已声明输入",
		Risk:            approvalmodel.RiskMedium,
		SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce},
		Metadata:        map[string]string{"provider": profile.Provider, "runtime_profile": string(profile.RuntimeProfile), "input_count": fmt.Sprint(len(inputs))},
	})
	if err != nil {
		return "", err
	}
	if decision.Type != approvalmodel.DecisionApproved && decision.Type != approvalmodel.DecisionApprovedForScope {
		return "", fmt.Errorf("approval %s: %s", decision.Type, decision.Reason)
	}

	manifest := workmodel.InputManifest{RunID: execution.Binding.Owner.RunID, BindingID: execution.Binding.ID, CreatedAt: time.Now().UTC()}
	if len(inputs) > 0 {
		refs, err := t.deps.InputResolver.ResolveInputs(ctx, inputs)
		if err != nil {
			return "", err
		}
		manifest, err = t.deps.InputStager.Stage(ctx, workcontract.StageRequest{Binding: execution.Binding, Sources: refs})
		if err != nil {
			return "", err
		}
	}
	result, err := t.deps.Runner.RunSandboxCommand(ctx, execservice.SandboxCommandRequest{
		Command: execmodel.Command{Command: command, Shell: execmodel.ShellAuto}, Binding: execution.Binding,
		Workspace: execution.Workspace, Sandbox: profile, Inputs: manifest, Timeout: sandboxTimeout(in.TimeoutMS),
	})
	if err != nil {
		return "", err
	}
	if result != nil && result.Result != nil && result.Result.ExitCode == 0 && len(result.Produced) > 0 && t.deps.Finalizer != nil {
		if _, err := t.deps.Finalizer.FinalizeRequired(ctx, execution.Binding.Owner.TenantID, execution.Binding.Owner.RunID); err != nil {
			return "", fmt.Errorf("自动发布 sandbox 交付物: %w", err)
		}
	}
	return sandboxResultJSON(result)
}

func mergeInputs(view workmodel.WorkspaceViewManifest, explicit []string) []string {
	result := make([]string, 0, len(view.Entries)+len(explicit))
	seen := make(map[string]struct{}, cap(result))
	add := func(value string) {
		value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
		if value == "" {
			return
		}
		key := value
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	for _, entry := range view.Entries {
		add(string(entry.Path))
	}
	for _, value := range explicit {
		add(value)
	}
	return result
}

func sandboxTimeout(raw int64) time.Duration {
	if raw <= 0 {
		return defaultTimeout
	}
	duration := time.Duration(raw) * time.Millisecond
	if duration > maxTimeout {
		return maxTimeout
	}
	return duration
}

func sandboxResultJSON(result *execservice.SandboxCommandResult) (string, error) {
	if result == nil || result.Result == nil {
		return "", fmt.Errorf("sandbox_exec返回空结果")
	}
	produced := make([]map[string]any, 0, len(result.Produced))
	for _, descriptor := range result.Produced {
		produced = append(produced, map[string]any{
			"resource_id": descriptor.ID, "logical_ref": descriptor.LogicalRef,
			"name": descriptor.ObservedName, "media_type": descriptor.MediaType, "size": descriptor.Size,
		})
	}
	payload := map[string]any{
		"ok": result.Result.ExitCode == 0, "environment": "sandbox", "cwd": result.Workspace.WorkDir,
		"exit_code": result.Result.ExitCode, "stdout": result.Result.Stdout, "stderr": result.Result.Stderr,
		"staged_inputs": result.StagedInputs, "produced_resources": produced,
		"staging_duration_ms": result.StagingTimeMS, "execution_duration_ms": result.ExecutionTimeMS,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

var _ toolcontract.Tool = (*Tool)(nil)
