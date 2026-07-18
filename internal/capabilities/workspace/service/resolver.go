package service

import (
	"context"
	"fmt"
	"strings"

	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

// ResolveBindingRequest 汇总候选选择与安全裁剪所需的可信快照。
type ResolveBindingRequest struct {
	Owner           execmodel.ExecutionOwnerRef
	Intent          workcontract.ExecutionIntent
	App             agentappmodel.EffectiveProfile
	ProductModes    []execmodel.WorkspaceMode
	PolicyModes     []execmodel.WorkspaceMode
	BackendModes    []execmodel.WorkspaceMode
	MaximumAccess   execmodel.WorkspaceAccess
	RequestedAccess execmodel.WorkspaceAccess
}

// WorkspaceResolver 生成不可变 ExecutionBinding。
type WorkspaceResolver struct{ ids IDGenerator }

func NewWorkspaceResolver(ids IDGenerator) (*WorkspaceResolver, error) {
	if ids == nil {
		return nil, fmt.Errorf("workspace resolver 缺少 id generator")
	}
	return &WorkspaceResolver{ids: ids}, nil
}

func (r *WorkspaceResolver) Resolve(ctx context.Context, req ResolveBindingRequest) (execmodel.ExecutionBinding, error) {
	if err := ctx.Err(); err != nil {
		return execmodel.ExecutionBinding{}, err
	}
	if strings.TrimSpace(req.Owner.RunID) == "" {
		return execmodel.ExecutionBinding{}, execcontract.NewError(execcontract.ErrCodeExecutionBindingRequired, fmt.Errorf("workspace resolver 缺少 run id"))
	}
	if req.MaximumAccess != execmodel.WorkspaceAccessReadOnly && req.MaximumAccess != execmodel.WorkspaceAccessReadWrite {
		return execmodel.ExecutionBinding{}, execcontract.NewError(execcontract.ErrCodeExecutionBindingConflict, fmt.Errorf("workspace resolver 缺少有效 access 上界"))
	}
	mode := selectCandidate(req.Intent, req.App.Workspace)
	if mode == "" {
		return execmodel.ExecutionBinding{}, execcontract.NewError(execcontract.ErrCodeWorkspaceModeNotAllowed, fmt.Errorf("无法从任务意图和 App 默认值确定 workspace mode"))
	}
	if (mode == execmodel.WorkspaceModeProject || req.App.Workspace.RequiresProject) && !req.Intent.HasProject {
		return execmodel.ExecutionBinding{}, execcontract.NewError(execcontract.ErrCodeWorkspaceModeNotAllowed, fmt.Errorf("所选模式要求已授权项目"))
	}
	for name, allowed := range map[string][]execmodel.WorkspaceMode{"app": req.App.Workspace.AllowedModes, "product": req.ProductModes, "policy": req.PolicyModes, "backend": req.BackendModes} {
		if len(allowed) > 0 && !containsMode(allowed, mode) {
			return execmodel.ExecutionBinding{}, execcontract.NewError(execcontract.ErrCodeWorkspaceModeNotAllowed, fmt.Errorf("模式 %s 不在 %s 允许范围", mode, name))
		}
	}
	access := req.RequestedAccess
	if access == "" {
		access = req.App.Workspace.DefaultAccess
	}
	if access == "" {
		access = execmodel.WorkspaceAccessReadOnly
	}
	if req.MaximumAccess == execmodel.WorkspaceAccessReadOnly && access == execmodel.WorkspaceAccessReadWrite {
		return execmodel.ExecutionBinding{}, execcontract.NewError(execcontract.ErrCodeExecutionBindingConflict, fmt.Errorf("请求写入超过有效授权上界"))
	}
	owner := req.Owner
	owner.AgentAppID = req.App.ID
	owner.AgentAppVersion = req.App.Version
	binding := execmodel.ExecutionBinding{ID: "binding-" + r.ids.Generate(), Mode: mode, Access: access, PathPolicy: defaultPathPolicy(mode), Owner: owner}
	if err := binding.Validate(); err != nil {
		return execmodel.ExecutionBinding{}, execcontract.NewError(execcontract.ErrCodeExecutionBindingConflict, err)
	}
	return binding, nil
}

func selectCandidate(intent workcontract.ExecutionIntent, spec agentappmodel.WorkspaceSpec) execmodel.WorkspaceMode {
	if intent.ExplicitMode != "" {
		return intent.ExplicitMode
	}
	if intent.RequiredMode != "" {
		return intent.RequiredMode
	}
	if intent.ModifyProject {
		return execmodel.WorkspaceModeProject
	}
	if intent.NeedsPersistentRun || spec.Persistent {
		return execmodel.WorkspaceModeSession
	}
	if intent.BoundedInputs && intent.BoundedOutputs {
		return execmodel.WorkspaceModeTask
	}
	return spec.DefaultMode
}

func containsMode(values []execmodel.WorkspaceMode, target execmodel.WorkspaceMode) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func defaultPathPolicy(mode execmodel.WorkspaceMode) execmodel.PathPolicy {
	if mode == execmodel.WorkspaceModeProject {
		return execmodel.PathPolicyPermissionOnly
	}
	return execmodel.PathPolicyStrictWorkspace
}
