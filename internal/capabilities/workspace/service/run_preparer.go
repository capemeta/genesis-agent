package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// RunPreparer 是 Run 工作空间唯一的创建控制面。
type RunPreparer struct {
	ids         IDGenerator
	resolver    *WorkspaceResolver
	stateRoots  workcontract.StateRootResolver
	provisioner workcontract.Provisioner
	manifests   workcontract.RunManifestStore
	now         func() time.Time
}

func NewRunPreparer(ids IDGenerator, resolver *WorkspaceResolver, stateRoots workcontract.StateRootResolver, provisioner workcontract.Provisioner, manifests workcontract.RunManifestStore) (*RunPreparer, error) {
	if ids == nil || resolver == nil || stateRoots == nil || provisioner == nil || manifests == nil {
		return nil, fmt.Errorf("run preparer 缺少 ids/resolver/stateRoots/provisioner/manifests")
	}
	return &RunPreparer{ids: ids, resolver: resolver, stateRoots: stateRoots, provisioner: provisioner, manifests: manifests, now: time.Now}, nil
}

func (p *RunPreparer) PrepareRun(ctx context.Context, req workcontract.PrepareRunRequest) (workmodel.PreparedRun, error) {
	if err := req.App.Validate(); err != nil {
		return workmodel.PreparedRun{}, fmt.Errorf("准备 Run: %w", err)
	}
	runID := "run-" + p.ids.Generate()
	owner := execmodel.ExecutionOwnerRef{TenantID: req.Scope.TenantID, ProjectID: req.Scope.ProjectID, UserID: req.Scope.UserID, SessionID: strings.TrimSpace(req.SessionID), AgentAppID: req.App.ID, AgentAppVersion: req.App.Version, RunID: runID, ParentRunID: strings.TrimSpace(req.ParentRunID)}
	if err := req.Subject.Validate(); err != nil {
		return workmodel.PreparedRun{}, fmt.Errorf("准备 Run execution subject: %w", err)
	}
	owner = req.Subject.ApplyTo(owner)
	binding, err := p.resolver.Resolve(ctx, ResolveBindingRequest{Owner: owner, Intent: req.Intent, App: req.App, ProductModes: req.ProductModes, PolicyModes: req.PolicyModes, BackendModes: req.BackendModes, MaximumAccess: req.MaximumAccess, RequestedAccess: req.RequestedAccess})
	if err != nil {
		return workmodel.PreparedRun{}, fmt.Errorf("解析 Run workspace binding: %w", err)
	}
	stateRoot, err := p.stateRoots.ResolveStateRoot(ctx, workcontract.StateRootRequest{RunID: runID, Mode: binding.Mode, Scope: req.Scope, ProjectRoot: req.ProjectRoot})
	if err != nil {
		return workmodel.PreparedRun{}, fmt.Errorf("解析 Run state root: %w", err)
	}
	if err := validateStateRoot(stateRoot, req.Scope); err != nil {
		return workmodel.PreparedRun{}, fmt.Errorf("解析 Run state root: %w", err)
	}
	if req.ProjectRoot != nil && req.ProjectRoot.Scope != req.Scope {
		return workmodel.PreparedRun{}, fmt.Errorf("准备 Run: project root scope 与 Run scope 不一致")
	}
	physical, err := p.provisioner.Prepare(ctx, workcontract.PrepareRequest{StateRoot: stateRoot, Binding: binding, ProjectDir: req.ProjectDir})
	if err != nil {
		return workmodel.PreparedRun{}, fmt.Errorf("准备 Run workspace: %w", err)
	}
	if err := validateProvisionedExecution(binding, physical); err != nil {
		return workmodel.PreparedRun{}, fmt.Errorf("准备 Run workspace: %w", err)
	}
	execution := workmodel.PreparedExecutionSnapshot{Binding: physical.Binding, Backend: physical.Backend, Workspace: physical.Workspace}
	manifest := workmodel.RunManifest{SchemaVersion: workmodel.RunManifestSchemaVersion, Revision: 1, RunID: runID, ParentRunID: owner.ParentRunID, Scope: req.Scope, AgentApp: req.App, ArtifactRequired: req.Intent.ArtifactRequired, StateRoot: stateRoot, ProjectRoot: req.ProjectRoot, ProjectDir: strings.TrimSpace(req.ProjectDir), Limits: workmodel.WorkspaceLimits{ProductModes: req.ProductModes, PolicyModes: req.PolicyModes, BackendModes: req.BackendModes, MaximumAccess: req.MaximumAccess}, Executions: []workmodel.PreparedExecutionSnapshot{execution}, CreatedAt: p.now().UTC()}
	if err := p.manifests.Create(ctx, manifest); err != nil {
		return workmodel.PreparedRun{}, fmt.Errorf("持久化 Run manifest: %w", err)
	}
	return workmodel.PreparedRun{Manifest: manifest, Execution: execution}, nil
}

// PrepareExecution 从已固化 manifest 继承身份和上界，并原子追加派生 execution。
func (p *RunPreparer) PrepareExecution(ctx context.Context, req workcontract.PrepareExecutionRequest) (workmodel.PreparedExecutionSnapshot, error) {
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok || strings.TrimSpace(prepared.Manifest.RunID) == "" {
		return workmodel.PreparedExecutionSnapshot{}, fmt.Errorf("准备派生 execution 缺少 Run manifest 上下文")
	}
	manifest, err := p.manifests.Get(ctx, prepared.Manifest.Scope.TenantID, prepared.Manifest.RunID)
	if err != nil {
		return workmodel.PreparedExecutionSnapshot{}, fmt.Errorf("读取 Run manifest: %w", err)
	}
	if err := req.Subject.Validate(); err != nil {
		return workmodel.PreparedExecutionSnapshot{}, fmt.Errorf("准备派生 execution subject: %w", err)
	}
	if req.Subject.Empty() {
		return workmodel.PreparedExecutionSnapshot{}, fmt.Errorf("准备派生 execution 缺少 subject")
	}
	baseExecution, found := findExecutionByBindingID(manifest, prepared.Execution.Binding.ID)
	if !found {
		return workmodel.PreparedExecutionSnapshot{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("上下文 execution binding 不属于权威 Run manifest"))
	}
	base := baseExecution.Binding.Owner
	owner := req.Subject.ApplyTo(base)
	if existing, found := findExecutionByOwner(manifest, owner, req.Backend); found {
		if err := validateReusableExecution(existing, req, manifest); err != nil {
			return workmodel.PreparedExecutionSnapshot{}, err
		}
		return existing, nil
	}
	// 项目授权是 Run manifest 中的可信事实。派生 execution 继承授权事实，
	// 但仍由自己的 binding/workspace 决定可见路径与读写边界。
	intent := req.Intent
	intent.HasProject = manifest.ProjectRoot != nil
	binding, err := p.resolver.Resolve(ctx, ResolveBindingRequest{Owner: owner, Intent: intent, App: manifest.AgentApp, ProductModes: manifest.Limits.ProductModes, PolicyModes: manifest.Limits.PolicyModes, BackendModes: manifest.Limits.BackendModes, MaximumAccess: manifest.Limits.MaximumAccess, RequestedAccess: req.RequestedAccess})
	if err != nil {
		return workmodel.PreparedExecutionSnapshot{}, err
	}
	physical, err := p.provisioner.Prepare(ctx, workcontract.PrepareRequest{StateRoot: manifest.StateRoot, Binding: binding, Backend: req.Backend, ProjectDir: manifest.ProjectDir, SkillDir: req.SkillDir})
	if err != nil {
		return workmodel.PreparedExecutionSnapshot{}, err
	}
	if err := validateProvisionedExecution(binding, physical); err != nil {
		return workmodel.PreparedExecutionSnapshot{}, err
	}
	if req.Backend.Kind != "" && physical.Backend != req.Backend {
		return workmodel.PreparedExecutionSnapshot{}, workcontract.NewError(workcontract.ErrCodeResourceBackendMismatch, fmt.Errorf("provisioner 返回的 backend 与 Harness 选择不一致"))
	}
	execution := workmodel.PreparedExecutionSnapshot{Binding: physical.Binding, Backend: physical.Backend, Workspace: physical.Workspace}
	const maxCASAttempts = 3
	for attempt := 0; attempt < maxCASAttempts; attempt++ {
		if _, err := p.manifests.AddExecution(ctx, manifest.Scope.TenantID, manifest.RunID, manifest.Revision, execution); err == nil {
			return execution, nil
		} else if !isResourceVersionConflict(err) {
			return workmodel.PreparedExecutionSnapshot{}, fmt.Errorf("追加 Run execution manifest: %w", err)
		}
		latest, getErr := p.manifests.Get(ctx, manifest.Scope.TenantID, manifest.RunID)
		if getErr != nil {
			return workmodel.PreparedExecutionSnapshot{}, fmt.Errorf("CAS 冲突后读取 Run manifest: %w", getErr)
		}
		if existing, found := findExecutionByOwner(latest, owner, req.Backend); found {
			if compatibleErr := validateReusableExecution(existing, req, latest); compatibleErr != nil {
				return workmodel.PreparedExecutionSnapshot{}, compatibleErr
			}
			return existing, nil
		}
		manifest = latest
	}
	return workmodel.PreparedExecutionSnapshot{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("追加 Run execution manifest 在 %d 次 CAS 后仍冲突", maxCASAttempts))
}

func validateStateRoot(root workmodel.StateRoot, scope workmodel.ResourceScope) error {
	if strings.TrimSpace(root.ID) == "" || strings.TrimSpace(root.Authority) == "" {
		return fmt.Errorf("state root 缺少 id/authority")
	}
	if root.Scope != scope {
		return fmt.Errorf("state root scope 与 Run scope 不一致")
	}
	return nil
}

func validateProvisionedExecution(expected execmodel.ExecutionBinding, prepared workcontract.PreparedExecution) error {
	if prepared.Binding != expected {
		return fmt.Errorf("provisioner 改写了不可变 execution binding")
	}
	if err := prepared.Backend.Validate(); err != nil {
		return fmt.Errorf("provisioner 返回无效 execution backend: %w", err)
	}
	if err := prepared.Workspace.ValidateFor(expected); err != nil {
		return fmt.Errorf("provisioner 返回无效 workspace: %w", err)
	}
	return nil
}

func isResourceVersionConflict(err error) bool {
	var classified *workcontract.Error
	return errors.As(err, &classified) && classified.Code == workcontract.ErrCodeResourceVersionConflict
}

func validateReusableExecution(existing workmodel.PreparedExecutionSnapshot, req workcontract.PrepareExecutionRequest, manifest workmodel.RunManifest) error {
	if req.Backend.Kind != "" && existing.Backend != req.Backend {
		return fmt.Errorf("派生 execution backend 不一致")
	}
	wantedMode := selectCandidate(req.Intent, manifest.AgentApp.Workspace)
	if wantedMode == "" || existing.Binding.Mode != wantedMode {
		return fmt.Errorf("派生 execution 主体已绑定到模式 %s，不能复用为 %s", existing.Binding.Mode, wantedMode)
	}
	wantedAccess := req.RequestedAccess
	if wantedAccess == "" {
		wantedAccess = manifest.AgentApp.Workspace.DefaultAccess
	}
	if wantedAccess == "" {
		wantedAccess = execmodel.WorkspaceAccessReadOnly
	}
	if wantedAccess == execmodel.WorkspaceAccessReadOnly && existing.Binding.Access == execmodel.WorkspaceAccessReadWrite {
		return fmt.Errorf("派生 execution 既有写权限超过本次只读请求，拒绝复用")
	}
	return nil
}

func (p *RunPreparer) GetRunManifest(ctx context.Context, tenantID, runID string) (workmodel.RunManifest, error) {
	return p.manifests.Get(ctx, strings.TrimSpace(tenantID), runID)
}

func findExecutionByOwner(manifest workmodel.RunManifest, owner execmodel.ExecutionOwnerRef, backend execmodel.ExecutionBackendRef) (workmodel.PreparedExecutionSnapshot, bool) {
	if owner.TaskID == "" && owner.SubAgentInstanceID == "" && owner.WorkflowStepID == "" && owner.MemberID == "" {
		return workmodel.PreparedExecutionSnapshot{}, false
	}
	for _, execution := range manifest.Executions {
		candidate := execution.Binding.Owner
		if candidate.SameExecutionSubject(owner) && (backend.Kind == "" || execution.Backend == backend) {
			return execution, true
		}
	}
	return workmodel.PreparedExecutionSnapshot{}, false
}

func findExecutionByBindingID(manifest workmodel.RunManifest, bindingID string) (workmodel.PreparedExecutionSnapshot, bool) {
	for _, execution := range manifest.Executions {
		if execution.Binding.ID == bindingID {
			return execution, true
		}
	}
	return workmodel.PreparedExecutionSnapshot{}, false
}
