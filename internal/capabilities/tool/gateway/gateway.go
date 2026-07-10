// Package gateway 提供工具调用网关。
package gateway

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	auditmodel "genesis-agent/internal/capabilities/audit/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
	tracecontract "genesis-agent/internal/capabilities/trace/contract"
	usagecontract "genesis-agent/internal/capabilities/usage/contract"
	usagemodel "genesis-agent/internal/capabilities/usage/model"
	"genesis-agent/internal/platform/logger/correl"
)

// AuditEvent 描述一次工具调用生命周期事件。
type AuditEvent struct {
	ToolName    string
	Phase       string
	Traits      tool.ToolTraits
	Allowed     bool
	StartedAt   time.Time
	CompletedAt time.Time
	Duration    time.Duration
	Error       string
	Metadata    map[string]string
}

// AuditSink 接收工具审计事件。
type AuditSink interface {
	RecordToolEvent(ctx context.Context, event AuditEvent)
}

// AuthorizationRequest 是 Gateway 级工具执行授权请求。
type AuthorizationRequest struct {
	ToolName string
	Params   string
	Info     *tool.Info
	Traits   tool.ToolTraits
}

// AuthorizationDecision 是 Gateway 级授权结果。
type AuthorizationDecision struct {
	Allowed  bool
	Reason   string
	Metadata map[string]string
}

// Authorizer 是工具执行前的统一授权入口。
type Authorizer interface {
	AuthorizeTool(ctx context.Context, request AuthorizationRequest) (AuthorizationDecision, error)
}

// Options 控制 Gateway 的治理依赖。
type Options struct {
	Locker     scheduler.ResourceLocker
	Tracer     tracecontract.Tracer
	Audit      AuditSink
	AuditSink  auditcontract.Sink
	UsageSink  usagecontract.Sink
	Authorizer Authorizer
}

// Gateway 在工具注册表外层执行可见性过滤、调度、追踪和审计。
type Gateway struct {
	registry  tool.Registry
	tools     profilemodel.ToolSet
	locker    scheduler.ResourceLocker
	tracer    tracecontract.Tracer
	audit     AuditSink
	auditSink auditcontract.Sink
	usageSink usagecontract.Sink
	authz     Authorizer
}

// New 创建工具网关。
func New(registry tool.Registry, tools profilemodel.ToolSet, options ...Options) *Gateway {
	g := &Gateway{registry: registry, tools: tools}
	if len(options) > 0 {
		g.locker = options[0].Locker
		g.tracer = options[0].Tracer
		g.audit = options[0].Audit
		g.auditSink = options[0].AuditSink
		g.usageSink = options[0].UsageSink
		g.authz = options[0].Authorizer
	}
	return g
}

// Register 透传工具注册。产品 bootstrap 仍应优先注册后再创建 Gateway。
func (g *Gateway) Register(t tool.Tool) { g.registry.Register(t) }

// Get 按名称获取已允许的工具。
func (g *Gateway) Get(name string) tool.Tool {
	name = strings.TrimSpace(name)
	if !g.isAllowed(name) {
		return nil
	}
	candidate := g.registry.Get(name)
	if candidate == nil || !isExecutable(candidate.GetInfo()) {
		return nil
	}
	return candidate
}

// IsRegistered 判断底层 Registry 是否已注册该工具（忽略 Profile 白名单）。
// CollisionGuard 用它区分「未注册」与「已注册但被 Profile 禁用」。
func (g *Gateway) IsRegistered(name string) bool {
	return g.registry.Get(strings.TrimSpace(name)) != nil
}

// Execute 执行工具，并通过统一网关做产品能力策略、基础调度、追踪和审计。
func (g *Gateway) Execute(ctx context.Context, name, params string) (result string, err error) {
	name = strings.TrimSpace(name)
	if !g.isAllowed(name) {
		return "", fmt.Errorf("工具 [%s] 未被当前产品 Profile 允许", name)
	}
	t := g.registry.Get(name)
	if t == nil {
		return "", fmt.Errorf("工具 [%s] 未注册", name)
	}
	info := t.GetInfo()
	traits := tool.TraitsOf(info)
	if !isExecutable(info) {
		return "", fmt.Errorf("工具 [%s] 不允许直接执行", name)
	}

	started := time.Now()
	g.record(ctx, AuditEvent{ToolName: name, Phase: "start", Traits: traits, Allowed: true, StartedAt: started})
	var span *tracecontract.Span
	if g.tracer != nil {
		span = g.tracer.StartSpan(ctx, "tool.execute", name+":"+started.Format(time.RFC3339Nano))
		span.Tags["tool.name"] = name
		span.Tags["tool.exposure"] = string(traits.Exposure)
		span.Tags["tool.read_only"] = fmt.Sprintf("%t", traits.ReadOnly)
		span.Tags["tool.needs_permission"] = fmt.Sprintf("%t", traits.NeedsPermission)
	}
	defer func() {
		if g.tracer != nil && span != nil {
			g.tracer.EndSpan(ctx, span, err)
		}
		event := AuditEvent{ToolName: name, Phase: "finish", Traits: traits, Allowed: err == nil, StartedAt: started, CompletedAt: time.Now()}
		event.Duration = event.CompletedAt.Sub(started)
		if err != nil {
			event.Error = err.Error()
		}
		g.record(ctx, event)
	}()

	if g.authz != nil {
		decision, authErr := g.authz.AuthorizeTool(ctx, AuthorizationRequest{ToolName: name, Params: params, Info: info, Traits: traits})
		if authErr != nil {
			return "", authErr
		}
		if !decision.Allowed {
			reason := strings.TrimSpace(decision.Reason)
			if reason == "" {
				reason = "tool execution denied"
			}
			return "", fmt.Errorf("工具 [%s] 未通过执行授权: %s", name, reason)
		}
	}

	release, err := g.acquire(ctx, name, traits)
	if err != nil {
		return "", err
	}
	defer release()
	return t.Execute(ctx, params)
}

// ListInfos 返回当前 Profile 可见的工具列表（动态描述已解析）。
func (g *Gateway) ListInfos() []*tool.Info {
	return g.ListInfosContext(context.Background())
}

// ListInfosContext 返回当前 Profile 可见的工具列表。
func (g *Gateway) ListInfosContext(ctx context.Context) []*tool.Info {
	infos := g.registry.ListInfos()
	allowed := make([]*tool.Info, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		name := strings.TrimSpace(info.Name)
		if !g.isAllowed(name) {
			continue
		}
		if !isVisible(info) {
			continue
		}
		allowed = append(allowed, tool.SnapshotForLLM(ctx, info))
	}
	sort.Slice(allowed, func(i, j int) bool { return allowed[i].Name < allowed[j].Name })
	return allowed
}

// FilterInfos 返回指定名称中被当前 Profile 允许的工具元信息。
func (g *Gateway) FilterInfos(names []string) []*tool.Info {
	return g.FilterInfosContext(context.Background(), names)
}

// FilterInfosContext 按名称过滤并解析动态描述。
func (g *Gateway) FilterInfosContext(ctx context.Context, names []string) []*tool.Info {
	filtered := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !g.isAllowed(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		filtered = append(filtered, name)
	}
	infos := make([]*tool.Info, 0, len(filtered))
	for _, name := range filtered {
		t := g.registry.Get(name)
		if t == nil {
			continue
		}
		info := t.GetInfo()
		if !isVisible(info) {
			continue
		}
		snap := tool.SnapshotForLLM(ctx, info)
		snap.Name = name
		infos = append(infos, snap)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos
}

// Names 返回当前 Profile 可见的工具名。
func (g *Gateway) Names() []string {
	infos := g.ListInfos()
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name)
	}
	return names
}

func (g *Gateway) acquire(ctx context.Context, name string, traits tool.ToolTraits) (func(), error) {
	if g.locker == nil {
		return func() {}, nil
	}
	mode := scheduler.LockWrite
	if traits.ReadOnly && traits.ConcurrencySafe {
		mode = scheduler.LockRead
	}
	return g.locker.Acquire(ctx, []scheduler.ResourceLock{{Scope: "tool", Key: name, Mode: mode}})
}

func (g *Gateway) record(ctx context.Context, event AuditEvent) {
	runID, sessionID, metadata := correl.Enrich(ctx, "", "", event.Metadata)
	if g.audit != nil {
		enriched := event
		enriched.Metadata = metadata
		g.audit.RecordToolEvent(ctx, enriched)
	}
	if g.auditSink != nil {
		_ = g.auditSink.Record(ctx, auditmodel.Event{
			Category:    "tool",
			Action:      event.ToolName + "." + event.Phase,
			Resource:    event.ToolName,
			RunID:       runID,
			SessionID:   sessionID,
			Severity:    auditSeverity(event),
			Allowed:     event.Allowed,
			Reason:      event.Error,
			StartedAt:   event.StartedAt,
			CompletedAt: event.CompletedAt,
			DurationMS:  event.Duration.Milliseconds(),
			Metadata:    auditMetadata(event, metadata),
		})
	}
	if g.usageSink != nil && event.Phase == "finish" {
		_ = g.usageSink.RecordToolUsage(ctx, usagemodel.ToolUsage{
			ToolName:    event.ToolName,
			Success:     event.Error == "",
			ReadOnly:    event.Traits.ReadOnly,
			DurationMS:  event.Duration.Milliseconds(),
			StartedAt:   event.StartedAt,
			CompletedAt: event.CompletedAt,
			RunID:       runID,
			SessionID:   sessionID,
			Metadata:    auditMetadata(event, metadata),
		})
	}
}

func auditSeverity(event AuditEvent) auditmodel.Severity {
	if event.Error != "" {
		return auditmodel.SeverityError
	}
	if !event.Allowed {
		return auditmodel.SeverityWarn
	}
	return auditmodel.SeverityInfo
}

func auditMetadata(event AuditEvent, enriched map[string]string) map[string]string {
	metadata := map[string]string{
		"tool.name":                      event.ToolName,
		"tool.phase":                     event.Phase,
		"tool.exposure":                  string(event.Traits.Exposure),
		"tool.read_only":                 fmt.Sprintf("%t", event.Traits.ReadOnly),
		"tool.concurrency_safe":          fmt.Sprintf("%t", event.Traits.ConcurrencySafe),
		"tool.needs_permission":          fmt.Sprintf("%t", event.Traits.NeedsPermission),
		"tool.requires_user_interaction": fmt.Sprintf("%t", event.Traits.RequiresUserInteraction),
	}
	for k, v := range enriched {
		metadata[k] = v
	}
	return metadata
}

func isVisible(info *tool.Info) bool {
	traits := tool.TraitsOf(info)
	return traits.Exposure == tool.ToolExposureDirect || traits.Exposure == tool.ToolExposureDeferred
}

func isExecutable(info *tool.Info) bool {
	return tool.TraitsOf(info).Exposure != tool.ToolExposureHidden
}

func (g *Gateway) isAllowed(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if matchesAny(g.tools.Disabled, name) {
		return false
	}
	if len(g.tools.Enabled) == 0 {
		return true
	}
	return matchesAny(g.tools.Enabled, name)
}

func matchesAny(patterns []string, name string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == "*" || pattern == name {
			return true
		}
		if strings.HasSuffix(pattern, ".*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
	}
	return false
}
