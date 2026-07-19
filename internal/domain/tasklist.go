// Package domain - 任务清单快照模型（会话消息/UI）
// TaskList 是会话级任务清单快照，以快照方式持久化到会话消息中。
// 每次修改生成新快照（version 递增），追加到会话消息历史，不原地修改。
package domain

import "time"

// TaskListItemStatus 任务清单条目执行状态
type TaskListItemStatus string

const (
	TaskListItemPending TaskListItemStatus = "pending" // ☐ 待开始
	TaskListItemDoing   TaskListItemStatus = "doing"   // ▶ 进行中
	TaskListItemDone    TaskListItemStatus = "done"    // ✓ 已完成
	TaskListItemSkipped TaskListItemStatus = "skipped" // - 已跳过
	TaskListItemFailed  TaskListItemStatus = "failed"  // ✗ 失败
)

// TaskListItem 任务清单中的单条任务
type TaskListItem struct {
	ID     string             `json:"id"`
	Text   string             `json:"text"`
	Status TaskListItemStatus `json:"status"`
	// DoneAt 完成时间（仅 done/skipped/failed 时有值）
	DoneAt *time.Time `json:"done_at,omitempty"`
	// Note 完成备注、跳过原因或失败原因
	Note string `json:"note,omitempty"`
}

// TaskList 任务清单快照
// 每次修改追加新快照到会话消息，不覆盖旧版本，保留完整审计轨迹。
// UI/Agent 始终使用最新版本（最后一条 task_list_snapshot 消息）。
type TaskList struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Title     string         `json:"title"`
	Summary   string         `json:"summary,omitempty"`
	Items     []TaskListItem `json:"items"`
	Version   int            `json:"version"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// DoneCount 返回已完成（done）条目数
func (p *TaskList) DoneCount() int {
	n := 0
	for _, item := range p.Items {
		if item.Status == TaskListItemDone {
			n++
		}
	}
	return n
}

// TotalCount 返回条目总数
func (p *TaskList) TotalCount() int {
	return len(p.Items)
}

// ProgressPct 返回完成百分比 [0, 100]
func (p *TaskList) ProgressPct() int {
	total := p.TotalCount()
	if total == 0 {
		return 0
	}
	return p.DoneCount() * 100 / total
}

// IsCompleted 判断清单是否全部完成（所有条目均为 done 或 skipped）
func (p *TaskList) IsCompleted() bool {
	if len(p.Items) == 0 {
		return false
	}
	for _, item := range p.Items {
		if item.Status == TaskListItemPending || item.Status == TaskListItemDoing || item.Status == TaskListItemFailed {
			return false
		}
	}
	return true
}

// TickItem 更新指定条目状态，返回新快照（version+1）。
// 若 itemID 不存在则返回 nil。
func (p *TaskList) TickItem(itemID string, status TaskListItemStatus, note string) *TaskList {
	newList := *p
	items := make([]TaskListItem, len(p.Items))
	copy(items, p.Items)
	found := false
	now := time.Now()
	for i, item := range items {
		if item.ID == itemID {
			items[i].Status = status
			items[i].Note = note
			if status == TaskListItemDone || status == TaskListItemSkipped || status == TaskListItemFailed {
				items[i].DoneAt = &now
			}
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	newList.Items = items
	newList.Version++
	newList.UpdatedAt = now
	return &newList
}
