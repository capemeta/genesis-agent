package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	plancontract "genesis-agent/internal/capabilities/plan/contract"
	planmodel "genesis-agent/internal/capabilities/plan/model"
	"genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/platform/contextutil"
)

// TodoReadTool 读取当前待办计划的工具
type TodoReadTool struct {
	planSvc plancontract.Service
}

// NewTodoReadTool 创建 TodoReadTool 实例
func NewTodoReadTool(svc plancontract.Service) tool.Tool {
	return &TodoReadTool{planSvc: svc}
}

func (t *TodoReadTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "todo_read",
		Description: "读取当前会话的待办任务清单 (TodoList)。自动过滤多余已完成的步骤以节约您的上下文 Token 空间。",
		Parameters: &tool.ParameterSchema{
			Type:       "object",
			Properties: map[string]*tool.ParameterSchema{},
			Required:   []string{},
		},
		Traits: tool.ToolTraits{
			Exposure:        tool.ToolExposureDirect,
			ReadOnly:        true,
			ConcurrencySafe: true,
			NeedsPermission: false,
		},
	}
}

func (t *TodoReadTool) Execute(ctx context.Context, _ string) (string, error) {
	sessionID, ok := contextutil.GetSessionID(ctx)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("context missing session_id")
	}

	plan, err := t.planSvc.GetPlan(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("fetch plan failed: %w", err)
	}
	if plan == nil || len(plan.Steps) == 0 {
		return `{"steps":[],"message":"当前无计划，请使用 todo_write 规划步骤"}`, nil
	}

	// 1. 进行 Token 剪枝 (Token Pruning)
	// 识别所有含有未完成子孙的 parent ID（防止出现子步骤进行中但父步骤已被隐藏的孤儿步骤）
	uncompletedParents := make(map[string]bool)
	for {
		changed := false
		for _, step := range plan.Steps {
			if step.Status != planmodel.StepStatusCompleted && step.ParentID != "" {
				if !uncompletedParents[step.ParentID] {
					uncompletedParents[step.ParentID] = true
					changed = true
				}
			}
			if uncompletedParents[step.ID] && step.ParentID != "" {
				if !uncompletedParents[step.ParentID] {
					uncompletedParents[step.ParentID] = true
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	var prunedSteps []planmodel.Step
	var completedSteps []planmodel.Step

	for _, step := range plan.Steps {
		if step.Status == planmodel.StepStatusCompleted {
			// 如果该已完成步骤含有未完成子孙，必须强制保留在主上下文列表中，不可归档
			if uncompletedParents[step.ID] {
				prunedSteps = append(prunedSteps, step)
			} else {
				completedSteps = append(completedSteps, step)
			}
		} else {
			prunedSteps = append(prunedSteps, step)
		}
	}

	archivedCount := 0
	const maxCompletedReferences = 3

	// 如果已完成步骤超过 3 项，自动归档过滤，仅保留最后 3 个
	if len(completedSteps) > maxCompletedReferences {
		archivedCount = len(completedSteps) - maxCompletedReferences
		completedSteps = completedSteps[archivedCount:]
	}

	// 合并已完成参考和未完成步骤
	finalSteps := append(completedSteps, prunedSteps...)

	// 2. 格式化输出
	type PrunedResponse struct {
		SessionID         string             `json:"session_id"`
		Steps             []planmodel.Step   `json:"steps"`
		LatestExplanation string             `json:"latest_explanation,omitempty"`
		Version           int64              `json:"version"`
		ArchivedCompleted int                `json:"archived_completed_count"`
		Message           string             `json:"message,omitempty"`
	}

	resp := PrunedResponse{
		SessionID:         plan.SessionID,
		Steps:             finalSteps,
		LatestExplanation: plan.LatestExplanation,
		Version:           plan.Version,
		ArchivedCompleted: archivedCount,
	}

	if archivedCount > 0 {
		resp.Message = fmt.Sprintf("温馨提示：共有 %d 项历史已完成任务已被自动归档归并以节约 Context 空间。", archivedCount)
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("marshal pruned response failed: %w", err)
	}

	return string(respBytes), nil
}
