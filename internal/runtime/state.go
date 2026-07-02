package runtime

import (
	"genesis-agent/internal/domain"
	"time"
)

// RunState 表示引擎在某一时刻的运行时状态快照
// 对应 AGENTS.md §4.1 RunEngine 状态定义
type RunState struct {
	RunID     string
	Status    domain.RunStatus
	Steps     []*domain.Step
	StartedAt time.Time
	UpdatedAt time.Time
}
