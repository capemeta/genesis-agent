package file

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"genesis-agent/internal/capabilities/usage/model"
	"genesis-agent/internal/platform/logger/correl"
)

// Sink 将用量事件以 jsonl 写入滚动 Writer。
type Sink struct {
	mu     sync.Mutex
	writer io.Writer
}

// NewSink 创建文件用量 Sink；writer 通常为 platform/logger.RotatingWriter。
func NewSink(writer io.Writer) *Sink {
	return &Sink{writer: writer}
}

// RecordToolUsage 写入一行 jsonl 用量事件；顶层强制提升 run_id/session_id。
func (s *Sink) RecordToolUsage(ctx context.Context, usage model.ToolUsage) error {
	if s == nil || s.writer == nil {
		return nil
	}
	runID, sessionID, metadata := correl.Enrich(ctx, usage.RunID, usage.SessionID, usage.Metadata)
	payload := map[string]any{
		"ts":          time.Now().Format(time.RFC3339),
		"type":        "tool.usage",
		"tool_name":   usage.ToolName,
		"success":     usage.Success,
		"read_only":   usage.ReadOnly,
		"duration_ms": usage.DurationMS,
	}
	if runID != "" {
		payload["run_id"] = runID
	}
	if sessionID != "" {
		payload["session_id"] = sessionID
	}
	if !usage.StartedAt.IsZero() {
		payload["started_at"] = usage.StartedAt.Format(time.RFC3339)
	}
	if !usage.CompletedAt.IsZero() {
		payload["completed_at"] = usage.CompletedAt.Format(time.RFC3339)
	}
	if len(metadata) > 0 {
		payload["metadata"] = scrubMetadata(metadata)
	}

	line, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化用量事件失败: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.writer.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("写入用量日志失败: %w", err)
	}
	return nil
}

func scrubMetadata(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		if lk == "authorization" || lk == "api_key" || lk == "apikey" || lk == "secret" ||
			lk == "password" || lk == "token" || lk == "cookie" || lk == "access_key" || lk == "secret_key" {
			out[k] = "[redacted]"
			continue
		}
		out[k] = v
	}
	return out
}
