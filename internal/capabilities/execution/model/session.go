package model

import (
	"sync"
	"time"
)

// SessionStatus 描述 PTY 会话的生命周期状态。
type SessionStatus string

const (
	SessionStatusPending   SessionStatus = "pending"
	SessionStatusRunning   SessionStatus = "running"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusFailed    SessionStatus = "failed"
)

// TerminalSession 描述伪终端交互式/后台命令行会话实体。
type TerminalSession struct {
	ID        string        `json:"id"`
	Command   string        `json:"command"`
	Status    SessionStatus `json:"status"`
	PID       int           `json:"pid"`
	LogPath   string        `json:"log_path"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`

	// 保护单个会话内状态变动的并发锁
	Mu sync.RWMutex `json:"-"`
}
