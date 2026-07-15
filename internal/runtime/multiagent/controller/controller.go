// Package controller 实现会话级子智能体控制平面。
package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
	"genesis-agent/internal/runtime/progress"
)

// Controller 是内存 Store 驱动的 Phase 1 控制器。持久化 Store 以后通过同一端口替换。
type Controller struct {
	engine  runtime.RunEngine
	limiter contract.SlotLimiter
	logger  logger.Logger

	mu        sync.RWMutex
	instances map[string]*entry
	nextID    uint64
}

type entry struct {
	instance  model.Instance
	cancel    context.CancelFunc
	slot      contract.SlotToken
	done      chan struct{}
	parentCtx context.Context
}

// New 创建控制器。
func New(engine runtime.RunEngine, limiter contract.SlotLimiter, log logger.Logger) (*Controller, error) {
	if engine == nil {
		return nil, fmt.Errorf("subagent RunEngine不能为空")
	}
	if limiter == nil {
		return nil, fmt.Errorf("subagent SlotLimiter不能为空")
	}
	if log == nil {
		log = logger.NewNop()
	}
	return &Controller{engine: engine, limiter: limiter, logger: log, instances: make(map[string]*entry)}, nil
}

// Spawn 预留并发槽后异步启动独立子 Run；调用方可立即 Wait。
func (c *Controller) Spawn(ctx context.Context, req contract.SpawnRequest) (result model.Instance, err error) {
	if req.Agent == nil {
		return result, fmt.Errorf("subagent Agent不能为空")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return result, fmt.Errorf("subagent prompt不能为空")
	}
	if req.Depth > 1 {
		return result, fmt.Errorf("agent depth limit reached: max=1；本层不可再委派，请自行完成")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		req.SessionID = strings.TrimSpace(req.ParentRunID)
	}
	if err := dispatchSubagentStart(ctx, req); err != nil {
		return result, err
	}
	token, err := c.limiter.Reserve(ctx, req.SessionID, req.Depth)
	if err != nil {
		return result, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = c.limiter.Release(token)
		}
	}()

	agentID := fmt.Sprintf("agent-%d", atomic.AddUint64(&c.nextID, 1))
	var childCtx context.Context
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		childCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	} else {
		childCtx, cancel = context.WithCancel(ctx)
	}
	childCtx = progress.WithSink(childCtx, func(progress.Event) {})
	childCtx = contextutil.WithSessionID(childCtx, req.SessionID)
	childCtx = contextutil.WithTenantID(childCtx, req.TenantID)
	instance := model.Instance{AgentID: agentID, ParentRunID: req.ParentRunID, SessionID: req.SessionID, Depth: req.Depth, SubagentType: req.SubagentType, Status: model.StatusRunning, CreatedAt: time.Now()}
	e := &entry{instance: instance, cancel: cancel, slot: token, done: make(chan struct{}), parentCtx: ctx}
	c.mu.Lock()
	c.instances[agentID] = e
	c.mu.Unlock()
	if err := c.limiter.Commit(token, agentID); err != nil {
		c.mu.Lock()
		delete(c.instances, agentID)
		c.mu.Unlock()
		cancel()
		return result, err
	}
	committed = true
	c.emit(ctx, progress.PhaseStart, instance, "启动子智能体")
	c.logger.Info("subagent spawn start", "run_id", req.ParentRunID, "session_id", req.SessionID, "agent_id", agentID, "agent", req.Agent.Name)
	go c.run(childCtx, req, e)
	return instance, nil
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
	run, err = c.engine.Start(ctx, domain.StartRunRequest{SessionID: req.SessionID, TenantID: req.TenantID, UserInput: req.Prompt, Agent: req.Agent})
}

func (c *Controller) finish(ctx context.Context, e *entry, run *domain.Run, err error) {
	c.mu.Lock()
	if run != nil {
		e.instance.ChildRunID = run.ID
		e.instance.Summary = truncate(run.FinalAnswer, 4000)
	}
	if err != nil {
		e.instance.Error = err.Error()
		if ctx.Err() != nil {
			e.instance.Status = model.StatusCancelled
		} else {
			e.instance.Status = model.StatusFailed
		}
	} else {
		e.instance.Status = model.StatusCompleted
	}
	now := time.Now()
	e.instance.FinishedAt = &now
	instance := e.instance
	c.mu.Unlock()
	_ = c.limiter.Release(e.slot)
	phase := progress.PhaseComplete
	summary := "子智能体完成"
	if instance.Status != model.StatusCompleted {
		phase, summary = progress.PhaseError, "子智能体未完成"
	}
	c.emit(e.parentCtx, phase, instance, summary)
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
		return model.Instance{}, err
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
func (c *Controller) Stop(_ context.Context, agentID string) error {
	e, err := c.entry(agentID)
	if err != nil {
		return err
	}
	e.cancel()
	return nil
}

// Get 返回实例快照，不等待其到达终态。
func (c *Controller) Get(_ context.Context, agentID string) (model.Instance, error) {
	e, err := c.entry(agentID)
	if err != nil {
		return model.Instance{}, err
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

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
