// Package model 定义任务清单与步骤的领域模型。
package model

import "time"

// StepStatus 定义待办事项的执行状态
type StepStatus string

const (
	StepStatusPending             StepStatus = "pending"
	StepStatusInProgress          StepStatus = "in_progress"
	StepStatusCompleted           StepStatus = "completed"
	StepStatusBlockedByApproval   StepStatus = "blocked_by_approval" // 人工审批阻断状态
)

// Priority 定义步骤的优先级
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
)

// Step 待办步骤实体
type Step struct {
	ID          string     `json:"id"`
	ParentID    string     `json:"parent_id,omitempty"` // 父步骤 ID，支持树拓扑结构与嵌套 Agent
	Title       string     `json:"title"`
	Status      StepStatus `json:"status"`
	Priority    Priority   `json:"priority,omitempty"`
	Assignee    string     `json:"assignee,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// RevisionLog 任务清单修改记录日志（追加审计模式）
type RevisionLog struct {
	Version     int64     `json:"version"`
	Explanation string    `json:"explanation,omitempty"`
	Operator    string    `json:"operator"` // "agent" 或 "user"
	Timestamp   time.Time `json:"timestamp"`
}

// TaskList 任务清单主快照实体
type TaskList struct {
	SessionID         string    `json:"session_id"`
	Steps             []Step    `json:"steps"`
	LatestExplanation string    `json:"latest_explanation,omitempty"`
	Version           int64     `json:"version"` // 用于并发防冲突乐观锁
	UpdatedAt         time.Time `json:"updated_at"`
}
