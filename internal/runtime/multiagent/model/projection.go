package model

import "time"

// ProjectionChannel 标识三端对同一子任务控制面事件的消费视角。
type ProjectionChannel string

const (
	ProjectionChannelCLI        ProjectionChannel = "cli"
	ProjectionChannelDesktop    ProjectionChannel = "desktop"
	ProjectionChannelEnterprise ProjectionChannel = "enterprise"
)

// ProjectionEventType 是产品投影层可订阅的稳定事件类型。
type ProjectionEventType string

const (
	ProjectionEventSpawned   ProjectionEventType = "spawned"
	ProjectionEventHeartbeat ProjectionEventType = "heartbeat"
	ProjectionEventStopped   ProjectionEventType = "stopped"
	ProjectionEventCompleted ProjectionEventType = "completed"
)

// ProjectionEvent 是 CLI 摘要、Desktop 节点视图、Enterprise 审计治理共享的最小事件协议。
// 事件只承载控制面摘要与已归约结果标识，不携带子 Run transcript 或未过滤工具输出。
type ProjectionEvent struct {
	Type        ProjectionEventType `json:"type"`
	Channel     ProjectionChannel   `json:"channel"`
	TenantID    string              `json:"tenant_id"`
	SessionID   string              `json:"session_id"`
	ParentRunID string              `json:"parent_run_id"`
	AgentID     string              `json:"agent_id"`
	ChildRunID  string              `json:"child_run_id,omitempty"`
	Status      Status              `json:"status"`
	ResultID    string              `json:"result_id,omitempty"`
	OccurredAt  time.Time           `json:"occurred_at"`
	Metadata    map[string]string   `json:"metadata,omitempty"`
}

// ProjectionQuery 仅允许按控制面标识筛选投影事件；不支持查询原始执行内容。
type ProjectionQuery struct {
	TenantID  string
	SessionID string
	AgentID   string
	Limit     int
}
