// Package execution 统一准备 L2 Workflow step 与 L3 CollaborationSpace member 的执行绑定。
package execution

import (
	"context"
	"fmt"
	"strings"

	agentappcontract "genesis-agent/internal/capabilities/agentapp/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// RuntimePolicy 是产品已经裁剪后的工作空间能力上界。
type RuntimePolicy struct {
	ProductModes  []execmodel.WorkspaceMode
	PolicyModes   []execmodel.WorkspaceMode
	BackendModes  []execmodel.WorkspaceMode
	MaximumAccess execmodel.WorkspaceAccess
}

// Coordinator 只编排主体身份；binding、Run ID、scope 和物理路径仍由 workspace 控制面生成。
type Coordinator struct {
	control workcontract.ControlPlane
	apps    agentappcontract.Resolver
	policy  RuntimePolicy
}

func NewCoordinator(control workcontract.ControlPlane, apps agentappcontract.Resolver, policy RuntimePolicy) (*Coordinator, error) {
	if control == nil {
		return nil, fmt.Errorf("multiagent execution coordinator 缺少 workspace control plane")
	}
	if policy.MaximumAccess == "" {
		policy.MaximumAccess = execmodel.WorkspaceAccessReadOnly
	}
	policy.ProductModes = append([]execmodel.WorkspaceMode(nil), policy.ProductModes...)
	policy.PolicyModes = append([]execmodel.WorkspaceMode(nil), policy.PolicyModes...)
	policy.BackendModes = append([]execmodel.WorkspaceMode(nil), policy.BackendModes...)
	return &Coordinator{control: control, apps: apps, policy: policy}, nil
}

// WorkflowStepRequest 描述 L2 App 内 step；step 默认使用独立 task_job 可写层。
type WorkflowStepRequest struct {
	StepID   string
	ReadOnly bool
}

// PrepareWorkflowStep 为同一 Run 内的 step 创建或幂等复用独立 execution。
func (c *Coordinator) PrepareWorkflowStep(ctx context.Context, req WorkflowStepRequest) (workmodel.PreparedExecutionSnapshot, error) {
	if strings.TrimSpace(req.StepID) == "" {
		return workmodel.PreparedExecutionSnapshot{}, fmt.Errorf("workflow step id 不能为空")
	}
	if _, _, err := c.authoritativeParent(ctx); err != nil {
		return workmodel.PreparedExecutionSnapshot{}, err
	}
	access := execmodel.WorkspaceAccessReadWrite
	if req.ReadOnly {
		access = execmodel.WorkspaceAccessReadOnly
	}
	return c.control.PrepareExecution(ctx, workcontract.PrepareExecutionRequest{
		Subject:         execmodel.ExecutionSubjectRef{WorkflowStepID: strings.TrimSpace(req.StepID)},
		Intent:          workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask, BoundedInputs: true, BoundedOutputs: true},
		RequestedAccess: access,
	})
}

// CollaborationMemberRequest 描述 L3 成员；成员 App ID 只用于解析受信 EffectiveProfile。
type CollaborationMemberRequest struct {
	CollaborationSpaceID string
	MemberID             string
	AppID                string
	ReadOnly             bool
	Intent               workcontract.ExecutionIntent
}

// PrepareCollaborationMember 为成员创建独立 Run/binding，不复用发起者 App 或可写 cwd。
func (c *Coordinator) PrepareCollaborationMember(ctx context.Context, req CollaborationMemberRequest) (workmodel.PreparedRun, error) {
	if c.apps == nil {
		return workmodel.PreparedRun{}, fmt.Errorf("collaboration member 缺少 Agent App resolver")
	}
	req.AppID = strings.TrimSpace(req.AppID)
	if req.AppID == "" {
		return workmodel.PreparedRun{}, fmt.Errorf("collaboration member 缺少明确 Agent App ID")
	}
	subject := execmodel.ExecutionSubjectRef{CollaborationSpaceID: strings.TrimSpace(req.CollaborationSpaceID), MemberID: strings.TrimSpace(req.MemberID)}
	if err := subject.Validate(); err != nil || subject.Empty() {
		if err == nil {
			err = fmt.Errorf("collaboration member subject 不能为空")
		}
		return workmodel.PreparedRun{}, err
	}
	parent, parentExecution, err := c.authoritativeParent(ctx)
	if err != nil {
		return workmodel.PreparedRun{}, err
	}
	app, err := c.apps.ResolveEffective(ctx, agentappcontract.ResolveRequest{AppID: req.AppID, Scope: parent.Scope})
	if err != nil {
		return workmodel.PreparedRun{}, fmt.Errorf("解析 collaboration member Agent App: %w", err)
	}
	intent := req.Intent
	if intent.ExplicitMode == "" && intent.RequiredMode == "" {
		intent.RequiredMode = execmodel.WorkspaceModeTask
		intent.BoundedInputs = true
		intent.BoundedOutputs = true
	}
	// HasProject 是授权事实，不接受编排请求自行声明。
	intent.HasProject = parent.ProjectRoot != nil
	access := execmodel.WorkspaceAccessReadWrite
	if req.ReadOnly {
		access = execmodel.WorkspaceAccessReadOnly
	}
	sessionID := parentExecution.Binding.Owner.SessionID
	return c.control.PrepareRun(ctx, workcontract.PrepareRunRequest{
		Scope: parent.Scope, SessionID: sessionID, ParentRunID: parent.RunID, Subject: subject,
		App: app, Intent: intent, ProjectRoot: parent.ProjectRoot, ProjectDir: parent.ProjectDir,
		ProductModes: c.policy.ProductModes, PolicyModes: c.policy.PolicyModes, BackendModes: c.policy.BackendModes,
		MaximumAccess: c.policy.MaximumAccess, RequestedAccess: access,
	})
}

func (c *Coordinator) authoritativeParent(ctx context.Context) (workmodel.RunManifest, workmodel.PreparedExecutionSnapshot, error) {
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok || strings.TrimSpace(prepared.Manifest.RunID) == "" {
		return workmodel.RunManifest{}, workmodel.PreparedExecutionSnapshot{}, fmt.Errorf("multiagent execution 缺少父 Run manifest 上下文")
	}
	manifest, err := c.control.GetRunManifest(ctx, prepared.Manifest.Scope.TenantID, prepared.Manifest.RunID)
	if err != nil {
		return workmodel.RunManifest{}, workmodel.PreparedExecutionSnapshot{}, fmt.Errorf("读取权威父 Run manifest: %w", err)
	}
	if manifest.Scope != prepared.Manifest.Scope {
		return workmodel.RunManifest{}, workmodel.PreparedExecutionSnapshot{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("父 Run scope 与权威 manifest 不一致"))
	}
	for _, execution := range manifest.Executions {
		if execution.Binding.ID == prepared.Execution.Binding.ID {
			return manifest, execution, nil
		}
	}
	return workmodel.RunManifest{}, workmodel.PreparedExecutionSnapshot{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("父 execution binding 不在权威 manifest"))
}
