package contract

import (
	"context"
	"time"

	"genesis-agent/internal/capabilities/tasklist/model"
)

// EventType 描述事件的类型
type EventType string

const (
	EventTaskListCreated EventType = "plan_created"
	EventTaskListUpdated EventType = "plan_updated"
)

// TaskListEvent 广播更新负载数据
type TaskListEvent struct {
	Type        EventType   `json:"type"`
	SessionID   string      `json:"session_id"`
	Plan        *model.TaskList `json:"plan"`
	Explanation string      `json:"explanation,omitempty"`
	Timestamp   time.Time   `json:"timestamp"`
}

// EventBroadcaster 事件通知广播契约接口
type EventBroadcaster interface {
	Broadcast(ctx context.Context, event TaskListEvent) error
}
