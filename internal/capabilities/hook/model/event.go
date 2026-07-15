// Package model 定义 Hook 能力域的稳定数据模型。
package model

// EventName 表示 Agent 生命周期中的 Hook 触发点。
type EventName string

const (
	EventRunStart          EventName = "RunStart"
	EventUserPromptSubmit  EventName = "UserPromptSubmit"
	EventPreToolUse        EventName = "PreToolUse"
	EventPostToolUse       EventName = "PostToolUse"
	EventPreSkillUse       EventName = "PreSkillUse"
	EventPostSkillUse      EventName = "PostSkillUse"
	EventStop              EventName = "Stop"
	EventRunComplete       EventName = "RunComplete"
	EventPreCompact        EventName = "PreCompact"
	EventSubagentStart     EventName = "SubagentStart"
	EventSubagentStop      EventName = "SubagentStop"
	EventPermissionRequest EventName = "PermissionRequest"
)

// Event 是一次 Hook 调度请求。Payload 会作为 command handler 的 stdin JSON 主体。
type Event struct {
	Name     EventName
	MatchKey string
	Payload  map[string]any
}

// Decision 是单个 Hook handler 的归一化输出。
type Decision struct {
	Continue           bool
	PermissionDecision string
	Reason             string
	UpdatedInput       map[string]any
	AdditionalContext  string
	SystemMessage      string
	ExitCode           int
	Err                error
}

// AggregateResult 是同一事件多个 handler 的保序聚合结果。
type AggregateResult struct {
	Blocked           bool
	BlockReason       string
	NeedApproval      bool
	UpdatedInput      map[string]any
	AdditionalContext []string
	SystemMessages    []string
	Warnings          []string
}

// IsBlockingEvent 表示该事件的决策可以阻断主流程。
func (e EventName) IsBlockingEvent() bool {
	switch e {
	case EventUserPromptSubmit, EventPreToolUse, EventPreSkillUse, EventStop, EventPreCompact, EventSubagentStart, EventPermissionRequest:
		return true
	default:
		return false
	}
}
