package model

import "time"

// ToolUsage 描述一次工具调用用量。
type ToolUsage struct {
	ToolName    string            `json:"tool_name"`
	Success     bool              `json:"success"`
	ReadOnly    bool              `json:"read_only"`
	DurationMS  int64             `json:"duration_ms"`
	StartedAt   time.Time         `json:"started_at"`
	CompletedAt time.Time         `json:"completed_at"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}
