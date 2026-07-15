package observability

import (
	"context"
	"fmt"

	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	auditmodel "genesis-agent/internal/capabilities/audit/model"
	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
	tracecontract "genesis-agent/internal/capabilities/trace/contract"
)

// Listener 将 MCP 生命周期事件写入 Audit / Trace。
type Listener struct {
	Audit  auditcontract.Sink
	Tracer tracecontract.Tracer
}

// OnMCPEvent 实现 contract.StateListener。
func (l *Listener) OnMCPEvent(ctx context.Context, event model.LifecycleEvent) {
	if l == nil {
		return
	}
	severity := auditmodel.SeverityInfo
	allowed := true
	switch event.Kind {
	case model.EventServerFailed:
		severity = auditmodel.SeverityError
		allowed = false
	case model.EventServerStarting:
		severity = auditmodel.SeverityInfo
	}
	if l.Audit != nil {
		_ = l.Audit.Record(ctx, auditmodel.Event{
			Category:  "mcp",
			Action:    string(event.Kind),
			Resource:  "mcp://" + event.Server,
			Severity:  severity,
			Allowed:   allowed,
			Reason:    event.State.Error,
			StartedAt: event.At,
			Metadata: map[string]string{
				"server":   event.Server,
				"status":   string(event.State.Status),
				"origin":   string(event.State.Origin),
				"required": fmt.Sprintf("%v", event.State.Required),
				"tools":    fmt.Sprintf("%d", event.State.ToolCount),
			},
		})
	}
	if l.Tracer != nil {
		span := l.Tracer.StartSpan(ctx, string(event.Kind), event.Server)
		var err error
		if event.State.Error != "" {
			err = fmt.Errorf("%s", event.State.Error)
		}
		l.Tracer.EndSpan(ctx, span, err)
	}
}

var _ contract.StateListener = (*Listener)(nil)
