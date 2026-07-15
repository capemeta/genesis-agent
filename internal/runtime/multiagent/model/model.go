// Package model 定义子智能体运行时的产品无关模型。
package model

import "time"

// Status 表示子智能体实例的生命周期状态。
type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Instance 是一次委派的运行态快照。
type Instance struct {
	AgentID      string
	ParentRunID  string
	SessionID    string
	Depth        int
	SubagentType string
	Status       Status
	Summary      string
	Error        string
	ChildRunID   string
	CreatedAt    time.Time
	FinishedAt   *time.Time
}
