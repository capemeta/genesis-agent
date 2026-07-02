package domain

import (
	"encoding/json"
	"time"
)

// TaskStatus 任务的生命周期状态
type TaskStatus string

const (
	TaskStatusPending         TaskStatus = "pending"
	TaskStatusRunning         TaskStatus = "running"
	TaskStatusWaitingApproval TaskStatus = "waiting_approval"
	TaskStatusWaitingEvent    TaskStatus = "waiting_event"
	TaskStatusCompleted       TaskStatus = "completed"
	TaskStatusFailed          TaskStatus = "failed"
	TaskStatusCancelled       TaskStatus = "cancelled"
)

// Task 要完成的业务任务，支持树形结构
type Task struct {
	ID             string
	TenantID       string
	WorkspaceID    string
	SessionID      string
	ParentTaskID   string // 子 Task 支持树形结构
	AssigneeID     string // AgentInstance ID
	CreatedByType  string // user / agent / scheduler / webhook
	CreatedByID    string // 发起者 ID
	TriggerEventID string
	Title          string
	Payload        json.RawMessage
	Status         TaskStatus
	Priority       int
	Deadline       *time.Time
	RetryCount     int
	MaxRetry       int
	ResourceAudit  // 嵌入标准审计字段
}
