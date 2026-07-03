package memory

import (
	"context"
	"sync"

	"genesis-agent/internal/capabilities/usage/model"
)

// Sink 是进程内用量收集器。
type Sink struct {
	mu    sync.Mutex
	tools []model.ToolUsage
}

// NewSink 创建内存用量 sink。
func NewSink() *Sink { return &Sink{} }

// RecordToolUsage 记录工具用量。
func (s *Sink) RecordToolUsage(_ context.Context, usage model.ToolUsage) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if usage.Metadata != nil {
		metadata := make(map[string]string, len(usage.Metadata))
		for k, v := range usage.Metadata {
			metadata[k] = v
		}
		usage.Metadata = metadata
	}
	s.tools = append(s.tools, usage)
	return nil
}

// ToolUsages 返回工具用量快照。
func (s *Sink) ToolUsages() []model.ToolUsage {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.ToolUsage, len(s.tools))
	copy(out, s.tools)
	return out
}
