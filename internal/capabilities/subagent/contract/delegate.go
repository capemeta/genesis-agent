// Package contract 定义子智能体委派网关的内部端口。
package contract

import (
	"context"

	"genesis-agent/internal/capabilities/subagent/model"
)

// 上下文快照模式（与 runtime/multiagent/contextsnapshot.Mode 对齐）。
const (
	SnapshotModeIsolated        = "isolated"
	SnapshotModeLastNTurns      = "last_n_turns"
	SnapshotModeFilteredHistory = "filtered_history"
	SnapshotModeSkillIsolated   = "skill_isolated"
)

// DelegateRequest 是 Task 网关的内部委派请求（非 LLM schema）。
type DelegateRequest struct {
	SubagentType string
	Prompt       string
	Description  string
	Background   bool
	MaxTurns     int
	MaxTokens    int64
	MaxToolCalls int
	TimeoutSec   int
	ForkContext  *bool
	AllowedTools []string
	// Definition 非空时跳过 Catalog（skill-fork 临时定义）。
	Definition *model.Definition
	// SnapshotMode 覆盖默认上下文模式；空则按 fork_context / isolated。
	SnapshotMode string
	PromptOrigin string
}

// Delegator 是固定 Task 网关的内部委派端口。
type Delegator interface {
	Delegate(ctx context.Context, req DelegateRequest) (string, error)
}
