package contract

import (
	"context"
	"time"

	"genesis-agent/internal/capabilities/plan/model"
)

// EventType 描述事件的类型
type EventType string

const (
	EventPlanCreated EventType = "plan_created"
	EventPlanUpdated EventType = "plan_updated"
)

// PlanEvent 广播更新负载数据
type PlanEvent struct {
	Type        EventType   `json:"type"`
	SessionID   string      `json:"session_id"`
	Plan        *model.Plan `json:"plan"`
	Explanation string      `json:"explanation,omitempty"`
	Timestamp   time.Time   `json:"timestamp"`
}

// EventBroadcaster 事件通知广播契约接口
type EventBroadcaster interface {
	Broadcast(ctx context.Context, event PlanEvent) error
}
