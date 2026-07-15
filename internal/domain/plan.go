// Package domain - 规划任务模型
// Plan 是 Agent 规划技能产出的结构化计划，以快照方式持久化到会话消息中。
// 每次修改生成新快照（version 递增），追加到会话消息历史，不原地修改。
package domain

import "time"

// PlanItemStatus 计划条目执行状态
type PlanItemStatus string

const (
	PlanItemPending PlanItemStatus = "pending"  // ☐ 待开始
	PlanItemDoing   PlanItemStatus = "doing"    // ▶ 进行中
	PlanItemDone    PlanItemStatus = "done"     // ✓ 已完成
	PlanItemSkipped PlanItemStatus = "skipped"  // - 已跳过
	PlanItemFailed  PlanItemStatus = "failed"   // ✗ 失败
)

// PlanItem 计划中的单条任务
type PlanItem struct {
	ID     string         `json:"id"`
	Text   string         `json:"text"`
	Status PlanItemStatus `json:"status"`
	// DoneAt 完成时间（仅 done/skipped/failed 时有值）
	DoneAt *time.Time `json:"done_at,omitempty"`
	// Note 完成备注、跳过原因或失败原因
	Note string `json:"note,omitempty"`
}

// Plan 计划快照
// 每次修改追加新快照到会话消息，不覆盖旧版本，保留完整审计轨迹。
// UI/Agent 始终使用最新版本（最后一条 plan_snapshot 消息）。
type Plan struct {
	ID        string     `json:"id"`
	SessionID string     `json:"session_id"`
	Title     string     `json:"title"`
	Summary   string     `json:"summary,omitempty"`
	Items     []PlanItem `json:"items"`
	Version   int        `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// DoneCount 返回已完成（done）条目数
func (p *Plan) DoneCount() int {
	n := 0
	for _, item := range p.Items {
		if item.Status == PlanItemDone {
			n++
		}
	}
	return n
}

// TotalCount 返回条目总数
func (p *Plan) TotalCount() int {
	return len(p.Items)
}

// ProgressPct 返回完成百分比 [0, 100]
func (p *Plan) ProgressPct() int {
	total := p.TotalCount()
	if total == 0 {
		return 0
	}
	return p.DoneCount() * 100 / total
}

// IsCompleted 判断计划是否全部完成（所有条目均为 done 或 skipped）
func (p *Plan) IsCompleted() bool {
	if len(p.Items) == 0 {
		return false
	}
	for _, item := range p.Items {
		if item.Status == PlanItemPending || item.Status == PlanItemDoing || item.Status == PlanItemFailed {
			return false
		}
	}
	return true
}

// TickItem 更新指定条目状态，返回新 Plan 快照（version+1）。
// 若 itemID 不存在则返回 nil。
func (p *Plan) TickItem(itemID string, status PlanItemStatus, note string) *Plan {
	newPlan := *p
	items := make([]PlanItem, len(p.Items))
	copy(items, p.Items)
	found := false
	now := time.Now()
	for i, item := range items {
		if item.ID == itemID {
			items[i].Status = status
			items[i].Note = note
			if status == PlanItemDone || status == PlanItemSkipped || status == PlanItemFailed {
				items[i].DoneAt = &now
			}
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	newPlan.Items = items
	newPlan.Version++
	newPlan.UpdatedAt = now
	return &newPlan
}
