package model

import "time"

// Severity 描述审计事件级别。
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// Event 是跨工具、权限、沙箱的统一审计事件。
type Event struct {
	ID          string            `json:"id,omitempty"`
	Category    string            `json:"category"`
	Action      string            `json:"action"`
	SubjectID   string            `json:"subject_id,omitempty"`
	Resource    string            `json:"resource,omitempty"`
	RunID       string            `json:"run_id,omitempty"`
	SessionID   string            `json:"session_id,omitempty"`
	Severity    Severity          `json:"severity"`
	Allowed     bool              `json:"allowed"`
	Reason      string            `json:"reason,omitempty"`
	StartedAt   time.Time         `json:"started_at,omitempty"`
	CompletedAt time.Time         `json:"completed_at,omitempty"`
	DurationMS  int64             `json:"duration_ms,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}
