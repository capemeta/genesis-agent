// Package service 提供 Hook 的默认调度、匹配与聚合实现。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	auditmodel "genesis-agent/internal/capabilities/audit/model"
	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	"genesis-agent/internal/capabilities/hook/model"
	tracecontract "genesis-agent/internal/capabilities/trace/contract"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/progress"
)

// Dispatcher 是并发安全的默认 Hook 调度器。
type Dispatcher struct {
	config  model.Config
	runners map[string]hookcontract.Runner
	audit   auditcontract.Sink
	tracer  tracecontract.Tracer
	scope   model.ScopeContext
}

type DispatcherOption func(*Dispatcher)

func WithAuditSink(sink auditcontract.Sink) DispatcherOption {
	return func(d *Dispatcher) { d.audit = sink }
}
func WithTracer(tracer tracecontract.Tracer) DispatcherOption {
	return func(d *Dispatcher) { d.tracer = tracer }
}
func WithDefaultScope(scope model.ScopeContext) DispatcherOption {
	return func(d *Dispatcher) { d.scope = scope }
}

// NewDispatcher 创建默认调度器。未配置 handler 时仍可安全作为 no-op 使用。
func NewDispatcher(config model.Config, runners ...hookcontract.Runner) *Dispatcher {
	dispatcher := &Dispatcher{config: config, runners: make(map[string]hookcontract.Runner)}
	for _, runner := range runners {
		if runner != nil {
			dispatcher.runners[runner.Kind()] = runner
		}
	}
	return dispatcher
}

// NewDispatcherWithOptions 创建带观测依赖的默认调度器。
func NewDispatcherWithOptions(config model.Config, options []DispatcherOption, runners ...hookcontract.Runner) *Dispatcher {
	dispatcher := NewDispatcher(config, runners...)
	for _, option := range options {
		if option != nil {
			option(dispatcher)
		}
	}
	return dispatcher
}

// Dispatch 选中匹配 handler，并行运行、按配置顺序聚合决策。
func (d *Dispatcher) Dispatch(ctx context.Context, event model.Event) (model.AggregateResult, error) {
	if d == nil || !d.config.IsEnabled() {
		return model.AggregateResult{}, nil
	}
	payload := enrichPayload(ctx, event)
	input, err := json.Marshal(payload)
	if err != nil {
		return model.AggregateResult{}, fmt.Errorf("序列化 Hook payload 失败: %w", err)
	}
	type task struct {
		index   int
		matcher string
		spec    model.HandlerSpec
	}
	tasks := make([]task, 0)
	decisions := make([]model.Decision, 0)
	scope := mergeScopeContext(d.scope, hookcontract.ScopeContextFromContext(ctx))
	for _, group := range d.config.Events[event.Name] {
		if !group.IsEnabled() || (d.config.AllowManagedOnly && !group.Managed && !groupHasBuiltin(group)) || !matches(group.Matcher, event.MatchKey) || !scopeMatches(group.Scope, scope) {
			continue
		}
		for _, spec := range group.Handlers {
			state := d.config.State[HandlerKey(event.Name, group.Matcher, spec)]
			if !effectiveEnabled(group, spec, state) || (d.config.AllowManagedOnly && !strings.EqualFold(spec.Type, "builtin") && !group.Managed && !spec.Managed) || !scopeMatches(spec.Scope, scope) {
				continue
			}
			if strings.EqualFold(spec.Type, "command") && !trusted(spec, state) {
				now := time.Now()
				reason := fmt.Sprintf("Hook %q 未通过 trusted_hash 校验", HandlerKey(event.Name, group.Matcher, spec))
				d.auditEvent(ctx, event, group.Matcher, spec, "rejected", false, reason, now, now)
				decisions = append(decisions, model.Decision{Continue: true, Err: fmt.Errorf("%s", reason)})
				continue
			}
			tasks = append(tasks, task{index: len(decisions), matcher: group.Matcher, spec: spec})
			decisions = append(decisions, model.Decision{})
		}
	}
	var wg sync.WaitGroup
	for _, item := range tasks {
		runner := d.runners[strings.ToLower(strings.TrimSpace(item.spec.Type))]
		if runner == nil {
			decisions[item.index] = model.Decision{Continue: true, Err: fmt.Errorf("未注册 Hook handler 类型 %q", item.spec.Type)}
			continue
		}
		wg.Add(1)
		go func(item task, runner hookcontract.Runner) {
			defer wg.Done()
			started := time.Now()
			d.auditEvent(ctx, event, item.matcher, item.spec, "start", true, "", started, time.Time{})
			progress.Emit(ctx, progress.Event{Kind: progress.KindHook, Phase: progress.PhaseStart, Component: "hook", Name: item.spec.Name, Summary: string(event.Name)})
			var span *tracecontract.Span
			if d.tracer != nil {
				span = d.tracer.StartSpan(ctx, "hook.run", HandlerKey(event.Name, item.matcher, item.spec))
				span.Tags["hook.event"] = string(event.Name)
				span.Tags["hook.handler_type"] = item.spec.Type
			}
			decision := runner.Run(ctx, item.spec, input)
			if d.tracer != nil && span != nil {
				d.tracer.EndSpan(ctx, span, decision.Err)
			}
			decisions[item.index] = decision
			d.auditEvent(ctx, event, item.matcher, item.spec, "complete", decision.Err == nil && decision.ExitCode != 2, decision.Reason, started, time.Now())
			phase, level := progress.PhaseComplete, progress.LevelInfo
			if decision.Err != nil {
				phase, level = progress.PhaseError, progress.LevelWarn
			}
			progress.Emit(ctx, progress.Event{Kind: progress.KindHook, Phase: phase, Level: level, Component: "hook", Name: item.spec.Name, Summary: string(event.Name)})
		}(item, runner)
	}
	wg.Wait()
	return aggregate(event, decisions), nil
}

func mergeScopeContext(base, override model.ScopeContext) model.ScopeContext {
	result := base
	if override.Channel != "" {
		result.Channel = override.Channel
	}
	if override.TenantID != "" {
		result.TenantID = override.TenantID
	}
	if override.ProjectID != "" {
		result.ProjectID = override.ProjectID
	}
	if override.AgentID != "" {
		result.AgentID = override.AgentID
	}
	if override.UserID != "" {
		result.UserID = override.UserID
	}
	if len(override.RoleIDs) > 0 {
		result.RoleIDs = append([]string(nil), override.RoleIDs...)
	}
	if override.Environment != "" {
		result.Environment = override.Environment
	}
	return result
}

func groupHasBuiltin(group model.HookSpec) bool {
	for _, spec := range group.Handlers {
		if strings.EqualFold(spec.Type, "builtin") {
			return true
		}
	}
	return false
}

func effectiveEnabled(group model.HookSpec, spec model.HandlerSpec, state model.HookState) bool {
	if !group.IsEnabled() || !spec.IsEnabled() {
		return false
	}
	return state.Enabled == nil || *state.Enabled
}

func trusted(spec model.HandlerSpec, state model.HookState) bool {
	expected := strings.TrimSpace(state.TrustedHash)
	if expected == "" {
		expected = strings.TrimSpace(spec.TrustedHash)
	}
	return expected != "" && strings.EqualFold(expected, HandlerFingerprint(spec))
}

func (d *Dispatcher) auditEvent(ctx context.Context, event model.Event, matcher string, spec model.HandlerSpec, phase string, allowed bool, reason string, started, completed time.Time) {
	if d.audit == nil {
		return
	}
	runID, _ := contextutil.GetRunID(ctx)
	sessionID, _ := contextutil.GetSessionID(ctx)
	metadata := map[string]string{"event": string(event.Name), "matcher": matcher, "handler_type": spec.Type, "handler_key": HandlerKey(event.Name, matcher, spec)}
	if !completed.IsZero() {
		metadata["duration_ms"] = fmt.Sprintf("%d", completed.Sub(started).Milliseconds())
	}
	_ = d.audit.Record(ctx, auditmodel.Event{Category: "hook", Action: string(event.Name) + "." + phase, Resource: spec.Name, RunID: runID, SessionID: sessionID, Severity: auditmodel.SeverityInfo, Allowed: allowed, Reason: reason, StartedAt: started, CompletedAt: completed, DurationMS: completed.Sub(started).Milliseconds(), Metadata: metadata})
}

func enrichPayload(ctx context.Context, event model.Event) map[string]any {
	payload := make(map[string]any, len(event.Payload)+4)
	for key, value := range event.Payload {
		payload[key] = value
	}
	payload["hook_event_name"] = event.Name
	payload["schema_version"] = "1"
	if value, ok := contextutil.GetRunID(ctx); ok {
		payload["run_id"] = value
	}
	if value, ok := contextutil.GetSessionID(ctx); ok {
		payload["session_id"] = value
	}
	if value, ok := contextutil.GetTenantID(ctx); ok {
		payload["tenant_id"] = value
	}
	return payload
}
