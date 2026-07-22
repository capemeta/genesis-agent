// Package contract 定义子智能体控制平面的最小端口。
package contract

import (
	"context"
	"time"

	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/multiagent/model"
)

// SpawnRequest 描述一次子智能体启动请求。
type SpawnRequest struct {
	SessionID    string
	TenantID     string
	ParentRunID  string
	Depth        int
	MaxDepth     int
	ReadOnly     bool
	SubagentType string
	Prompt       string
	Agent        *domain.Agent
	Timeout      time.Duration
	Budget       *TreeBudget
	Inputs       []workmodel.ResourceRef
	// SkillQA* 来自 Skill frontmatter qa:；经 EvidenceQAHints 注入子 Run，
	// 仅在产物证据建约时写入 Spec（不再按 Intent 预建交付契约）。
	SkillQAPolicy      string
	SkillQAEnforcement string
}

// SlotToken 是限流器创建的不可透明预留凭据。
type SlotToken string

// SlotLimiter 统一三端的会话级并发槽语义。
type SlotLimiter interface {
	Reserve(ctx context.Context, sessionID string, depth int) (SlotToken, error)
	Commit(token SlotToken, agentID string) error
	Release(token SlotToken) error
}

// Controller 是 Task 工具依赖的最小控制平面。
type Controller interface {
	Spawn(ctx context.Context, request SpawnRequest) (model.Instance, error)
	Resume(ctx context.Context, agentID, prompt string) (model.Instance, error)
	Wait(ctx context.Context, agentID string) (model.Instance, error)
	Stop(ctx context.Context, agentID string) error
	Get(ctx context.Context, agentID string) (model.Instance, error)
}
