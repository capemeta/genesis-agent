// Package controller 实现会话级子智能体控制平面。
package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
	"genesis-agent/internal/runtime/multiagent/contextsnapshot"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
	"genesis-agent/internal/runtime/multiagent/projection"
	"genesis-agent/internal/runtime/multiagent/result"
	"genesis-agent/internal/runtime/progress"
)

// Controller 是内存 Store 驱动的 Phase 1 控制器。持久化 Store 以后通过同一端口替换。
type Controller struct {
	engine    runtime.RunEngine
	limiter   contract.SlotLimiter
	logger    logger.Logger
	reducer   result.Reducer
	projector result.Projector
	store     contract.InstanceStore
	proj      contract.ProjectionSink
	workspace WorkspaceRuntime

	mu        sync.RWMutex
	instances map[string]*entry
	nextID    uint64
	idPrefix  string
}

type entry struct {
	instance  model.Instance
	request   contract.SpawnRequest
	cancel    context.CancelFunc
	slot      contract.SlotToken
	done      chan struct{}
	parentCtx context.Context
	manifest  *result.ManifestRegistry
}

// WorkspaceRuntime 为每个子 Run 重新解析独立 binding，禁止继承父级可写 cwd。
type WorkspaceRuntime struct {
	Preparer      workcontract.ControlPlane
	ProjectRoot   *workmodel.ResourceRef
	ProjectDir    string
	ProductModes  []execmodel.WorkspaceMode
	PolicyModes   []execmodel.WorkspaceMode
	BackendModes  []execmodel.WorkspaceMode
	MaximumAccess execmodel.WorkspaceAccess
	// IntentResolver 仍可用于其它工作区推断；ArtifactRuns 保留给显式 DeclaredDeliverable 注入。
	// 交付门禁改为证据驱动：子写出可交付产物后由 FinalizeRequired 建约并交付，不再按 Prompt 预建 Spec。
	IntentResolver workcontract.IntentResolver
	ArtifactRuns   artifactcontract.RunInitializer
}

// New 创建控制器。
func New(engine runtime.RunEngine, limiter contract.SlotLimiter, log logger.Logger, options ...Option) (*Controller, error) {
	if engine == nil {
		return nil, fmt.Errorf("subagent RunEngine不能为空")
	}
	if limiter == nil {
		return nil, fmt.Errorf("subagent SlotLimiter不能为空")
	}
	if log == nil {
		log = logger.NewNop()
	}
	controller := &Controller{engine: engine, limiter: limiter, logger: log, reducer: result.NewReducer(), projector: result.NewProjector(nil), store: newMemoryStore(), proj: projection.NewNopSink(), instances: make(map[string]*entry), idPrefix: newIDPrefix()}
	for _, option := range options {
		option(controller)
	}
	return controller, nil
}

// WithInstanceStore 注入产品级实例 Store。
func WithInstanceStore(store contract.InstanceStore) Option {
	return func(controller *Controller) {
		if store != nil {
			controller.store = store
		}
	}
}

// Option 扩展 Controller 的产品无关依赖。
type Option func(*Controller)

// WithWorkspaceRuntime 注入子 Run 工作空间控制面。
func WithWorkspaceRuntime(value WorkspaceRuntime) Option {
	return func(controller *Controller) { controller.workspace = value }
}

// WithResultPipeline 注入可测试的终态归约与交付投影。
func WithResultPipeline(reducer result.Reducer, projector result.Projector) Option {
	return func(controller *Controller) {
		controller.reducer = reducer
		controller.projector = projector
	}
}

// WithProjectionSink 注入三端产品投影事件消费者。
func WithProjectionSink(sink contract.ProjectionSink) Option {
	return func(controller *Controller) {
		if sink != nil {
			controller.proj = sink
		}
	}
}

// Spawn 预留并发槽后异步启动独立子 Run；调用方可立即 Wait。
func (c *Controller) Spawn(ctx context.Context, req contract.SpawnRequest) (model.Instance, error) {
	var empty model.Instance
	if req.Agent == nil {
		return empty, fmt.Errorf("subagent Agent不能为空")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return empty, fmt.Errorf("subagent prompt不能为空")
	}
	ctx, parent, err := c.resolveParentContext(ctx, req.ParentRunID)
	if err != nil {
		return empty, err
	}
	req.ParentRunID = parent.Manifest.RunID
	req.TenantID = parent.Manifest.Scope.TenantID
	req.SessionID = parent.Execution.Binding.Owner.SessionID
	if req.Depth <= 0 {
		req.Depth = 1
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = 1
	}
	if req.MaxDepth > 2 {
		return empty, fmt.Errorf("agent max_depth limit reached: hard max=2")
	}
	if req.Depth > req.MaxDepth {
		return empty, fmt.Errorf("agent depth limit reached: max=%d；本层不可再委派，请自行完成", req.MaxDepth)
	}
	if err := dispatchSubagentStart(ctx, req); err != nil {
		return empty, err
	}
	token, err := c.limiter.Reserve(ctx, req.SessionID, req.Depth)
	if err != nil {
		return empty, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = c.limiter.Release(token)
		}
	}()

	agentID := fmt.Sprintf("agent-%s-%d", c.idPrefix, atomic.AddUint64(&c.nextID, 1))
	childBase := contextsnapshot.WithoutParentSnapshot(ctx)
	var childCtx context.Context
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		childCtx, cancel = context.WithTimeout(childBase, req.Timeout)
	} else {
		childCtx, cancel = context.WithCancel(childBase)
	}
	// 默认 no-op：子 Run 内部 KindRun/KindTool 不得串入父时间线。
	// Task 可通过 progress.WithChildBridge 显式桥接（带 Depth/SubAgentID 标签）供 CLI 展示。
	if bridge := progress.ChildBridgeFromContext(ctx); bridge != nil {
		childCtx = progress.WithSink(childCtx, bridge)
	} else {
		childCtx = progress.WithSink(childCtx, func(progress.Event) {})
	}
	manifest := result.NewManifestRegistry()
	childCtx = result.WithManifestRegistry(childCtx, manifest)
	childCtx = contract.WithDelegationDepth(childCtx, req.Depth)
	childCtx = contract.WithMaxDelegationDepth(childCtx, req.MaxDepth)
	childCtx = contract.WithDelegationReadOnly(childCtx, req.ReadOnly)
	childCtx = contract.WithDelegationTools(childCtx, toolNames(req.Agent.Tools))
	childCtx = contract.WithTreeBudget(childCtx, req.Budget)
	childCtx = contextutil.WithSessionID(childCtx, req.SessionID)
	childCtx = contextutil.WithTenantID(childCtx, req.TenantID)
	instance := model.Instance{AgentID: agentID, ParentRunID: req.ParentRunID, SessionID: req.SessionID, TenantID: req.TenantID, Depth: req.Depth, SubagentType: req.SubagentType, Status: model.StatusRunning, CreatedAt: time.Now()}
	e := &entry{instance: instance, request: req, cancel: cancel, slot: token, done: make(chan struct{}), parentCtx: ctx, manifest: manifest}
	if err := c.limiter.Commit(token, agentID); err != nil {
		cancel()
		return instance, err
	}
	stored, created, err := c.store.SaveIfInvocationAbsent(ctx, contract.StoredInstance{Instance: instance, Request: req})
	if err != nil {
		cancel()
		return empty, fmt.Errorf("保存 subagent 实例失败: %w", err)
	}
	if !created {
		cancel()
		return stored.Instance, nil
	}
	c.mu.Lock()
	c.instances[agentID] = e
	c.mu.Unlock()
	committed = true
	c.emit(ctx, progress.PhaseStart, instance, "启动子智能体")
	c.emitProjection(ctx, model.ProjectionEventSpawned, instance)
	c.logger.Info("subagent spawn start", "run_id", req.ParentRunID, "session_id", req.SessionID, "agent_id", agentID, "agent", req.Agent.Name)
	go c.run(childCtx, req, e)
	return instance, nil
}

func (c *Controller) resolveParentContext(ctx context.Context, parentRunID string) (context.Context, workmodel.PreparedRun, error) {
	if prepared, ok := workcontract.PreparedRunFromContext(ctx); ok && strings.TrimSpace(prepared.Manifest.RunID) != "" {
		return ctx, prepared, nil
	}
	if c.workspace.Preparer == nil || strings.TrimSpace(parentRunID) == "" {
		return ctx, workmodel.PreparedRun{}, fmt.Errorf("subagent 缺少父 Run 工作空间快照")
	}
	tenantID, ok := contextutil.GetTenantID(ctx)
	if !ok || strings.TrimSpace(tenantID) == "" {
		return ctx, workmodel.PreparedRun{}, fmt.Errorf("恢复父 Run manifest 缺少 tenant_id")
	}
	manifest, err := c.workspace.Preparer.GetRunManifest(ctx, tenantID, parentRunID)
	if err != nil {
		return ctx, workmodel.PreparedRun{}, fmt.Errorf("恢复父 Run manifest: %w", err)
	}
	if manifest.RunID != parentRunID || len(manifest.Executions) == 0 {
		return ctx, workmodel.PreparedRun{}, fmt.Errorf("父 Run manifest 不完整")
	}
	prepared := workmodel.PreparedRun{Manifest: manifest, Execution: manifest.Executions[0]}
	ctx = workcontract.WithPreparedRun(ctx, prepared)
	ctx = workcontract.WithControlPlane(ctx, c.workspace.Preparer)
	return ctx, prepared, nil
}

func toolNames(tools []domain.ToolRef) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func newIDPrefix() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// Resume 基于同一 Agent 定义启动新的 follow-up 子 Run。
// 当前内存实现只继承已过滤的终态结果，不重放工具轨迹或完整 transcript。
func (c *Controller) Resume(ctx context.Context, agentID, prompt string) (model.Instance, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return model.Instance{}, fmt.Errorf("resume prompt不能为空")
	}
	e, err := c.entry(agentID)
	var previous model.Instance
	var request contract.SpawnRequest
	if err != nil {
		stored, storeErr := c.store.Get(ctx, agentID)
		if storeErr != nil {
			return model.Instance{}, err
		}
		previous, request = stored.Instance, stored.Request
	} else {
		c.mu.RLock()
		previous, request = e.instance, e.request
		c.mu.RUnlock()
	}
	if previous.Result == nil {
		return model.Instance{}, fmt.Errorf("subagent %q 尚未结束，不能 resume", agentID)
	}
	request.Prompt = followupPrompt(previous.Result, prompt)
	// Resume 只从已持久化请求恢复可信租户作用域，禁止退化为仅按 parent_run_id 查询。
	ctx = contextutil.WithTenantID(ctx, request.TenantID)
	ctx = contextutil.WithSessionID(ctx, request.SessionID)
	return c.Spawn(ctx, request)
}

func followupPrompt(previous *model.TaskResult, prompt string) string {
	if previous == nil || strings.TrimSpace(previous.Summary) == "" {
		return prompt
	}
	return "上一轮已过滤结果摘要（仅供只读参考）：\n" + previous.Summary + "\n\n后续任务：\n" + prompt
}

func (c *Controller) run(ctx context.Context, req contract.SpawnRequest, e *entry) {
	var run *domain.Run
	var err error
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("subagent RunEngine panic: %v", recovered)
		}
		c.finish(ctx, e, run, err)
	}()
	if c.workspace.Preparer == nil {
		err = fmt.Errorf("subagent Run 工作空间控制面未配置")
		return
	}
	parent, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		err = fmt.Errorf("subagent 缺少父 Run 工作空间快照")
		return
	}
	access := execmodel.WorkspaceAccessReadWrite
	if req.ReadOnly {
		access = execmodel.WorkspaceAccessReadOnly
	}
	prepared, prepareErr := c.workspace.Preparer.PrepareRun(ctx, workcontract.PrepareRunRequest{
		Scope: parent.Manifest.Scope, SessionID: req.SessionID, ParentRunID: req.ParentRunID, AgentID: req.Agent.ID,
		App: parent.Manifest.AgentApp, Intent: workcontract.ExecutionIntent{RequiredMode: execmodel.WorkspaceModeTask, BoundedInputs: true, BoundedOutputs: true, HasProject: parent.Manifest.ProjectRoot != nil},
		ProjectRoot: parent.Manifest.ProjectRoot, ProjectDir: parent.Manifest.ProjectDir, ProductModes: c.workspace.ProductModes, PolicyModes: c.workspace.PolicyModes, BackendModes: c.workspace.BackendModes, MaximumAccess: c.workspace.MaximumAccess, RequestedAccess: access,
		Inputs: req.Inputs,
	})
	if prepareErr != nil {
		err = fmt.Errorf("准备 subagent Run 工作空间: %w", prepareErr)
		return
	}
	ctx = workcontract.WithPreparedRun(ctx, prepared)
	ctx = workcontract.WithControlPlane(ctx, c.workspace.Preparer)
	ctx, err = c.prepareChildArtifactContext(ctx, req, prepared)
	if err != nil {
		return
	}
	run, err = c.engine.Start(ctx, domain.StartRunRequest{RunID: prepared.Manifest.RunID, SessionID: req.SessionID, TenantID: req.TenantID, UserInput: req.Prompt, Agent: req.Agent})
}

// prepareChildArtifactContext 为子 Run 注入产物证据建约所需的可信 QA 偏好。
// 交付 Spec 不再按 Prompt/Intent 预建：只读无产物可直接完成；写出 office 交付文件后由 FinalizeRequired 建约并交付。
func (c *Controller) prepareChildArtifactContext(ctx context.Context, req contract.SpawnRequest, prepared workmodel.PreparedRun) (context.Context, error) {
	if req.InvocationBinding.ID != "" {
		// InvocationBinding 记录父 Run 发起调用时的不可变解析事实；子 Run 身份由
		// PreparedRun / ExecutionBinding 表达，禁止复制同一 binding_id 后篡改 RunID。
		ctx = skillcontract.WithInvocationBinding(ctx, req.InvocationBinding)
	}
	if len(req.Deliverables) > 0 {
		if c.workspace.ArtifactRuns == nil {
			return ctx, fmt.Errorf("SKILL_DELIVERABLE_REQUIRED: 子Run缺少声明式Deliverable初始化器")
		}
		if err := c.workspace.ArtifactRuns.InitializeRun(ctx, artifactcontract.RunInitializationRequest{
			TenantID: req.TenantID, RunID: prepared.Manifest.RunID, Deliverables: append([]artifactcontract.DeclaredDeliverable(nil), req.Deliverables...),
		}); err != nil {
			return ctx, fmt.Errorf("初始化子Run DeliverableSpec: %w", err)
		}
	}
	if _, ok := artifactcontract.CompletionPolicyFromContext(ctx); !ok && !req.ReadOnly {
		c.logger.Warn("子 Run 上下文缺少完成门禁，产物证据交付可能静默不触发", "agent_id", req.Agent.ID, "run_id", prepared.Manifest.RunID)
	}
	return ctx, nil
}

func (c *Controller) finish(ctx context.Context, e *entry, run *domain.Run, err error) {
	c.mu.RLock()
	instance := e.instance
	c.mu.RUnlock()
	if run != nil {
		instance.ChildRunID = run.ID
	}
	if err != nil {
		instance.Error = err.Error()
		if ctx.Err() != nil {
			instance.Status = model.StatusCancelled
		} else {
			instance.Status = model.StatusFailed
		}
	} else if run != nil && run.Status == domain.RunStatusCancelled {
		instance.Status = model.StatusCancelled
	} else if run != nil && run.Status == domain.RunStatusFailed {
		instance.Status = model.StatusFailed
	} else {
		instance.Status = model.StatusCompleted
	}
	manifest, findings := e.manifest.Snapshot()
	resultCtx := context.WithoutCancel(e.parentCtx)
	record := c.reducer.Reduce(resultCtx, result.TerminalCandidate{
		AgentID:      instance.AgentID,
		SubagentType: instance.SubagentType,
		Run:          run,
		Err:          err,
		Cancelled:    instance.Status == model.StatusCancelled,
		Manifest:     manifest,
		Findings:     findings,
	})
	projected := c.projector.Project(resultCtx, record)
	instance.Result = &projected
	instance.Summary = projected.Summary
	var adoptionErr error
	if e.request.InvocationBinding.Result.Kind == skillmodel.ResultKindDeliverables {
		if run == nil || projected.Status != model.ResultStatusCompleted {
			adoptionErr = fmt.Errorf("Skill子Run未达到可接纳的completed终态")
		} else if policy, ok := artifactcontract.CompletionPolicyFromContext(resultCtx); !ok {
			adoptionErr = fmt.Errorf("Skill子Run缺少CompletionPolicy，禁止接纳交付物")
		} else if decision, evalErr := policy.EvaluateCompletion(resultCtx, run.TenantID, run.ID); evalErr != nil {
			adoptionErr = fmt.Errorf("校验Skill子Run交付完成状态: %w", evalErr)
		} else if !decision.Complete {
			adoptionErr = fmt.Errorf("Skill子Run交付门禁未满足: missing=%v pending_qa=%v failures=%v", decision.MissingDeliverableIDs, decision.PendingQAIDs, decision.FailureCodes)
		}
	}
	for _, art := range projected.Artifacts {
		if adoptionErr != nil {
			break
		}
		candidateID := art.CandidateID
		if candidateID == "" {
			candidateID = art.ResourceID
		}
		if candidateID == "" || run == nil {
			continue
		}
		// 父子边界唯一的跨 Run 接纳点：把子交付候选显式、版本锁定地接纳进父作用域，
		// 供父随后按资源所属 backend 只读引用（view_image 等）。QA/中间物已在归约层剔除，不会到这里。
		adoptions, configured := artifactcontract.AdoptionStoreFromContext(resultCtx)
		if !configured {
			adoptionErr = fmt.Errorf("Skill子Run缺少AdoptionStore，禁止跨Run接纳交付物")
			break
		}
		if _, adoptErr := adoptions.Adopt(artifactcontract.AdoptionRecord{
			ConsumerTenantID: instance.TenantID,
			ConsumerRunID:    instance.ParentRunID,
			ProducedID:       candidateID,
			OwnerTenantID:    run.TenantID,
			OwnerRunID:       run.ID,
			AgentID:          instance.AgentID,
			ContentHash:      art.ContentHash,
			Role:             art.Role,
		}); adoptErr != nil {
			adoptionErr = fmt.Errorf("接纳子交付候选 %s: %w", candidateID, adoptErr)
			break
		}
	}
	if adoptionErr != nil {
		instance.Status = model.StatusFailed
		instance.Error = adoptionErr.Error()
		projected.Status = model.ResultStatusFailed
		projected.Error = &model.ResultError{Code: "artifact_adoption_failed", Message: adoptionErr.Error(), Retryable: true}
		projected.Artifacts = nil
		projected.NextAction = "修复父子产物接纳存储后重试；父 Run 不得直接扫描子工作区。"
		instance.Result = &projected
		instance.Summary = projected.Summary
	}
	if projected.Error != nil {
		instance.Error = projected.Error.Message
	}
	now := time.Now()
	instance.FinishedAt = &now
	c.mu.Lock()
	e.instance = instance
	c.mu.Unlock()
	if err := c.store.Save(resultCtx, contract.StoredInstance{Instance: instance, Request: e.request}); err != nil {
		c.logger.Error("保存 subagent 终态失败", "agent_id", instance.AgentID, "error", err)
	}
	_ = c.limiter.Release(e.slot)
	phase := progress.PhaseComplete
	summary := "子智能体完成"
	if instance.Status != model.StatusCompleted {
		phase, summary = progress.PhaseError, "子智能体未完成"
	}
	c.emit(e.parentCtx, phase, instance, summary)
	eventType := model.ProjectionEventCompleted
	if instance.Status == model.StatusCancelled {
		eventType = model.ProjectionEventStopped
	}
	c.emitProjection(resultCtx, eventType, instance)
	c.dispatchSubagentStop(e.parentCtx, instance)
	c.logger.Info("subagent finished", "run_id", instance.ParentRunID, "session_id", instance.SessionID, "agent_id", instance.AgentID, "status", instance.Status, "error", instance.Error)
	close(e.done)
}

func dispatchSubagentStart(ctx context.Context, req contract.SpawnRequest) error {
	dispatcher := hookcontract.FromContext(ctx)
	if dispatcher == nil {
		return nil
	}
	result, err := dispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventSubagentStart, MatchKey: req.SubagentType, Payload: map[string]any{
		"subagent_type": req.SubagentType,
		"parent_run_id": req.ParentRunID,
		"session_id":    req.SessionID,
		"depth":         req.Depth,
	}})
	if err != nil {
		return fmt.Errorf("执行 SubagentStart Hook 失败: %w", err)
	}
	hookcontract.AppendAdditionalContext(ctx, result.AdditionalContext...)
	if result.NeedApproval {
		return fmt.Errorf("SubagentStart Hook 要求人工审批")
	}
	if result.Blocked {
		return fmt.Errorf("子智能体启动被 Hook 阻断: %s", result.BlockReason)
	}
	return nil
}

func (c *Controller) dispatchSubagentStop(ctx context.Context, instance model.Instance) {
	dispatcher := hookcontract.FromContext(ctx)
	if dispatcher == nil {
		return
	}
	result, err := dispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventSubagentStop, MatchKey: instance.SubagentType, Payload: map[string]any{
		"subagent_type": instance.SubagentType,
		"parent_run_id": instance.ParentRunID,
		"child_run_id":  instance.ChildRunID,
		"session_id":    instance.SessionID,
		"agent_id":      instance.AgentID,
		"status":        string(instance.Status),
		"error":         instance.Error,
	}})
	if err != nil {
		c.logger.Warn("执行 SubagentStop Hook 失败", "agent_id", instance.AgentID, "error", err)
		return
	}
	hookcontract.AppendAdditionalContext(ctx, result.AdditionalContext...)
}

// Wait 等待指定实例到达终态。
func (c *Controller) Wait(ctx context.Context, agentID string) (model.Instance, error) {
	e, err := c.entry(agentID)
	if err != nil {
		stored, storeErr := c.store.Get(ctx, agentID)
		if storeErr != nil {
			return model.Instance{}, err
		}
		if stored.Instance.Status == model.StatusRunning {
			return model.Instance{}, fmt.Errorf("subagent %q 正在其他进程运行，当前进程不能等待其内存事件", agentID)
		}
		return stored.Instance, nil
	}
	select {
	case <-ctx.Done():
		_ = c.Stop(context.Background(), agentID)
		return model.Instance{}, ctx.Err()
	case <-e.done:
		c.mu.RLock()
		defer c.mu.RUnlock()
		return e.instance, nil
	}
}

// Stop 取消运行中的实例；终态由运行协程统一写入。
func (c *Controller) Stop(ctx context.Context, agentID string) error {
	e, err := c.entry(agentID)
	if err != nil {
		stored, storeErr := c.store.Get(ctx, agentID)
		if storeErr != nil {
			return err
		}
		if stored.Instance.Status != model.StatusRunning {
			return nil
		}
		return fmt.Errorf("subagent %q 正在其他进程运行，当前进程不能直接取消；请在持有该任务的进程执行 TaskStop", agentID)
	}
	e.cancel()
	return nil
}

// Get 返回实例快照，不等待其到达终态。
func (c *Controller) Get(ctx context.Context, agentID string) (model.Instance, error) {
	e, err := c.entry(agentID)
	if err != nil {
		stored, storeErr := c.store.Get(ctx, agentID)
		if storeErr != nil {
			return model.Instance{}, err
		}
		return stored.Instance, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return e.instance, nil
}

func (c *Controller) entry(agentID string) (*entry, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e := c.instances[strings.TrimSpace(agentID)]
	if e == nil {
		return nil, fmt.Errorf("subagent %q 不存在", agentID)
	}
	return e, nil
}

func (c *Controller) emit(ctx context.Context, phase progress.Phase, instance model.Instance, summary string) {
	metadata := map[string]string{
		"session_id":    instance.SessionID,
		"parent_run_id": instance.ParentRunID,
		"agent_id":      instance.AgentID,
		"subagent_type": instance.SubagentType,
		"status":        string(instance.Status),
	}
	if instance.ChildRunID != "" {
		metadata["child_run_id"] = instance.ChildRunID
	}
	progress.Emit(ctx, progress.Event{Kind: progress.KindSubAgent, Phase: phase, RunID: instance.ParentRunID, Component: "subagent", Name: "Task", Summary: summary, Metadata: metadata})
}

func (c *Controller) emitProjection(ctx context.Context, eventType model.ProjectionEventType, instance model.Instance) {
	if c.proj == nil {
		return
	}
	metadata := map[string]string{
		"subagent_type": instance.SubagentType,
	}
	resultID := ""
	if instance.Result != nil {
		resultID = instance.Result.ResultID
	}
	if err := c.proj.EmitProjection(ctx, model.ProjectionEvent{
		Type:        eventType,
		TenantID:    instance.TenantID,
		SessionID:   instance.SessionID,
		ParentRunID: instance.ParentRunID,
		AgentID:     instance.AgentID,
		ChildRunID:  instance.ChildRunID,
		Status:      instance.Status,
		ResultID:    resultID,
		OccurredAt:  time.Now(),
		Metadata:    metadata,
	}); err != nil {
		c.logger.Warn("投影 subagent 事件失败", "agent_id", instance.AgentID, "event", string(eventType), "error", err)
	}
}
