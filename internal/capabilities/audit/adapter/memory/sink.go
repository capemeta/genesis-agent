package memory

import (
	"context"
	"sync"

	"genesis-agent/internal/capabilities/audit/model"
)

// Sink 是进程内审计事件收集器，适用于 CLI/Desktop 开发态和测试。
type Sink struct {
	mu     sync.Mutex
	events []model.Event
}

// NewSink 创建内存审计 sink。
func NewSink() *Sink { return &Sink{} }

// Record 记录事件快照。
func (s *Sink) Record(_ context.Context, event model.Event) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.Metadata != nil {
		metadata := make(map[string]string, len(event.Metadata))
		for k, v := range event.Metadata {
			metadata[k] = v
		}
		event.Metadata = metadata
	}
	s.events = append(s.events, event)
	return nil
}

// Events 返回事件快照。
func (s *Sink) Events() []model.Event {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Event, len(s.events))
	copy(out, s.events)
	return out
}
