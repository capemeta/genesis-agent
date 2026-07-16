package projection

import (
	"context"
	"sync"
	"time"

	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

type nopSink struct{}

func NewNopSink() contract.ProjectionSink { return nopSink{} }

func (nopSink) EmitProjection(context.Context, model.ProjectionEvent) error { return nil }

// MemorySink 是三端本地投影和测试可复用的轻量事件收集器。
type MemorySink struct {
	channel model.ProjectionChannel
	mu      sync.RWMutex
	events  []model.ProjectionEvent
}

func NewMemorySink(channel model.ProjectionChannel) *MemorySink {
	return &MemorySink{channel: channel}
}

func (s *MemorySink) EmitProjection(ctx context.Context, event model.ProjectionEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if event.Channel == "" {
		event.Channel = s.channel
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, cloneEvent(event))
	return nil
}

func (s *MemorySink) Events() []model.ProjectionEvent {
	items, _ := s.ListProjectionEvents(context.Background(), model.ProjectionQuery{})
	return items
}

// ListProjectionEvents 返回已经归约的控制面事件，默认保留最近 100 条。
func (s *MemorySink) ListProjectionEvents(ctx context.Context, query model.ProjectionQuery) ([]model.ProjectionEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.ProjectionEvent, 0, len(s.events))
	for _, event := range s.events {
		if query.TenantID != "" && event.TenantID != query.TenantID {
			continue
		}
		if query.SessionID != "" && event.SessionID != query.SessionID {
			continue
		}
		if query.AgentID != "" && event.AgentID != query.AgentID {
			continue
		}
		out = append(out, cloneEvent(event))
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

var _ contract.ProjectionReader = (*MemorySink)(nil)

func cloneEvent(event model.ProjectionEvent) model.ProjectionEvent {
	if len(event.Metadata) > 0 {
		copied := make(map[string]string, len(event.Metadata))
		for key, value := range event.Metadata {
			copied[key] = value
		}
		event.Metadata = copied
	}
	return event
}
