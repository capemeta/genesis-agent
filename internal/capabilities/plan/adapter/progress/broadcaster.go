// Package progressbc 实现 plan.EventBroadcaster 接口，
// 将计划变更事件转换为 progress.Event{Kind: KindPlan} 通过 context 中的 Sink 发出。
//
// 这是连接"规划能力域"和"进度事件总线"的适配器，
// CLI TUI / Desktop / Enterprise 端只需订阅 progress.KindPlan 事件即可渲染计划卡片。
package progressbc

import (
	"context"
	"encoding/json"
	"time"

	plancontract "genesis-agent/internal/capabilities/plan/contract"
	planmodel "genesis-agent/internal/capabilities/plan/model"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/progress"
)

// Broadcaster 实现 plancontract.EventBroadcaster。
// 将 PlanEvent 转换为 domain.Plan，序列化后通过 progress.Emit 广播给订阅方。
type Broadcaster struct{}

// New 创建 Broadcaster 实例。
func New() *Broadcaster {
	return &Broadcaster{}
}

// Broadcast 将计划事件转换并通过进度总线广播。
// 若 context 中无 progress.Sink，为空操作，不影响主流程。
func (b *Broadcaster) Broadcast(ctx context.Context, event plancontract.PlanEvent) error {
	plan := convertToDomainPlan(event)
	if plan == nil {
		return nil
	}
	detail, err := json.Marshal(plan)
	if err != nil {
		return nil // 序列化失败不中断主流程
	}

	blockType := "update"
	if event.Type == plancontract.EventPlanCreated {
		blockType = "create"
	}

	progress.Emit(ctx, progress.Event{
		Kind:      progress.KindPlan,
		Phase:     progress.PhaseProgress,
		BlockType: blockType,
		Summary:   plan.Title,
		Detail:    string(detail),
		Time:      time.Now(),
	})
	return nil
}

// convertToDomainPlan 将 plan/model.Plan 转换为 domain.Plan 供各端渲染。
// plan/model.Plan 无 Title 字段，使用 LatestExplanation 或默认值填充。
func convertToDomainPlan(event plancontract.PlanEvent) *domain.Plan {
	p := event.Plan
	if p == nil {
		return nil
	}

	items := make([]domain.PlanItem, len(p.Steps))
	for i, step := range p.Steps {
		item := domain.PlanItem{
			ID:     step.ID,
			Text:   step.Title,
			Status: convertStepStatus(step.Status),
			Note:   step.Notes,
		}
		if step.Status == planmodel.StepStatusCompleted && !step.UpdatedAt.IsZero() {
			t := step.UpdatedAt
			item.DoneAt = &t
		}
		items[i] = item
	}

	title := p.LatestExplanation
	if title == "" {
		title = "当前任务计划"
	}
	// 标题不宜过长；截取前 20 个 rune
	titleRunes := []rune(title)
	if len(titleRunes) > 20 {
		title = string(titleRunes[:20]) + "…"
	}

	return &domain.Plan{
		ID:        p.SessionID, // 一 Session 一计划，以 SessionID 为 PlanID
		SessionID: p.SessionID,
		Title:     title,
		Items:     items,
		Version:   int(p.Version),
		CreatedAt: p.UpdatedAt, // plan/model 无 CreatedAt，退而求其次
		UpdatedAt: p.UpdatedAt,
	}
}

// convertStepStatus 将 plan/model.StepStatus 映射为 domain.PlanItemStatus。
func convertStepStatus(s planmodel.StepStatus) domain.PlanItemStatus {
	switch s {
	case planmodel.StepStatusInProgress:
		return domain.PlanItemDoing
	case planmodel.StepStatusCompleted:
		return domain.PlanItemDone
	case planmodel.StepStatusBlockedByApproval:
		// 审批阻断：显示为待开始（卡片上审批流另有专属卡处理）
		return domain.PlanItemPending
	default:
		return domain.PlanItemPending
	}
}
