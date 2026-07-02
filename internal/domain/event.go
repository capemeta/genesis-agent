package domain

import (
	"encoding/json"
	"time"
)

// EventType 事件类型
type EventType string

const (
	EventTypeUserMessage      EventType = "user_message"
	EventTypeApprovalGranted  EventType = "approval_granted"
	EventTypeApprovalRejected EventType = "approval_rejected"
	EventTypeWebhook          EventType = "webhook"
	EventTypeScheduler        EventType = "scheduler"
	EventTypeAgentCompleted   EventType = "agent_completed"
	EventTypeFileUploaded     EventType = "file_uploaded"
)

// EventStatus 事件处理状态
type EventStatus string

const (
	EventStatusPending    EventStatus = "pending"
	EventStatusProcessing EventStatus = "processing"
	EventStatusProcessed  EventStatus = "processed"
	EventStatusFailed     EventStatus = "failed"
)

// Event 所有触发动作统一路由的事件模型
type Event struct {
	ID          string
	TenantID    string
	WorkspaceID string
	EventType   EventType
	SourceType  string
	SourceID    string
	Payload     json.RawMessage
	Status      EventStatus
	OwnerID     string    // 事件触发者
	CreatedAt   time.Time // 只写表，无 UpdatedAt
}
