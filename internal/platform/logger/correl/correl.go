package correl

import (
	"context"
	"strings"

	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger"
)

// Enrich 用 context 与 metadata 补齐 run_id/session_id；优先显式字段，其次 context，再次 metadata。
func Enrich(ctx context.Context, runID, sessionID string, metadata map[string]string) (string, string, map[string]string) {
	ctxRun, ctxSession := contextutil.CorrelationIDs(ctx)
	runID = firstNonEmpty(runID, ctxRun, metadataValue(metadata, "run_id"))
	sessionID = firstNonEmpty(sessionID, ctxSession, metadataValue(metadata, "session_id"))
	if runID == "" && sessionID == "" {
		return "", "", metadata
	}
	out := cloneMap(metadata)
	if runID != "" {
		out["run_id"] = runID
	}
	if sessionID != "" {
		out["session_id"] = sessionID
	}
	return runID, sessionID, out
}

// AttachLogger 将 context 中的关联键附加到 Logger（agent 通道跨组件一致）。
func AttachLogger(ctx context.Context, log logger.Logger) logger.Logger {
	if log == nil {
		return logger.NewNop()
	}
	runID, sessionID := contextutil.CorrelationIDs(ctx)
	args := make([]any, 0, 4)
	if runID != "" {
		args = append(args, "run_id", runID)
	}
	if sessionID != "" {
		args = append(args, "session_id", sessionID)
	}
	if len(args) == 0 {
		return log
	}
	return log.With(args...)
}

func metadataValue(metadata map[string]string, key string) string {
	if metadata == nil {
		return ""
	}
	return strings.TrimSpace(metadata[key])
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in)+2)
	for k, v := range in {
		out[k] = v
	}
	return out
}
