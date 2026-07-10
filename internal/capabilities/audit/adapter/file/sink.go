package file

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"genesis-agent/internal/capabilities/audit/model"
	"genesis-agent/internal/platform/logger/correl"
)

// Sink 将审计事件以 jsonl 写入滚动 Writer。
type Sink struct {
	mu     sync.Mutex
	writer io.Writer
}

// NewSink 创建文件审计 Sink；writer 通常为 platform/logger.RotatingWriter。
func NewSink(writer io.Writer) *Sink {
	return &Sink{writer: writer}
}

// Record 写入一行 jsonl 事件；顶层强制提升 run_id/session_id 便于跨通道检索。
func (s *Sink) Record(ctx context.Context, event model.Event) error {
	if s == nil || s.writer == nil {
		return nil
	}
	runID, sessionID, metadata := correl.Enrich(ctx, event.RunID, event.SessionID, event.Metadata)
	payload := map[string]any{
		"ts":       time.Now().Format(time.RFC3339),
		"type":     event.Category,
		"action":   event.Action,
		"resource": event.Resource,
		"allowed":  event.Allowed,
		"severity": event.Severity,
	}
	if runID != "" {
		payload["run_id"] = runID
	}
	if sessionID != "" {
		payload["session_id"] = sessionID
	}
	if event.ID != "" {
		payload["id"] = event.ID
	}
	if event.SubjectID != "" {
		payload["subject_id"] = event.SubjectID
	}
	if event.Reason != "" {
		payload["reason"] = event.Reason
	}
	if !event.StartedAt.IsZero() {
		payload["started_at"] = event.StartedAt.Format(time.RFC3339)
	}
	if !event.CompletedAt.IsZero() {
		payload["completed_at"] = event.CompletedAt.Format(time.RFC3339)
	}
	if event.DurationMS > 0 {
		payload["duration_ms"] = event.DurationMS
	}
	if len(metadata) > 0 {
		payload["metadata"] = scrubMetadata(metadata)
	}

	line, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化审计事件失败: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.writer.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("写入审计日志失败: %w", err)
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
