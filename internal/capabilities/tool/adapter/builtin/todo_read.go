package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	tasklistcontract "genesis-agent/internal/capabilities/tasklist/contract"
	tasklistmodel "genesis-agent/internal/capabilities/tasklist/model"
	tasklistprompt "genesis-agent/internal/capabilities/tasklist/prompt"
	"genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	"genesis-agent/internal/platform/contextutil"
)

// TodoReadTool 读取当前待办计划的工具
type TodoReadTool struct {
	planSvc tasklistcontract.Service
}

// NewTodoReadTool 创建 TodoReadTool 实例
func NewTodoReadTool(svc tasklistcontract.Service) tool.Tool {
	return &TodoReadTool{planSvc: svc}
}

func (t *TodoReadTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "todo_read",
		Description: tasklistprompt.ToolTodoReadDescription,
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

func (t *TodoReadTool) Execute(ctx context.Context, params string) (string, error) {
	if err := toolparam.DecodeOptional(params, &struct{}{}); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	sessionID, ok := contextutil.GetSessionID(ctx)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("context missing session_id")
	}

	plan, err := t.planSvc.GetTaskList(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("fetch plan failed: %w", err)
	}
	if plan == nil || len(plan.Steps) == 0 {
		return `{"steps":[],"message":"当前无任务清单，请使用 todo_write 创建步骤"}`, nil
	}

	// 1. 进行 Token 剪枝 (Token Pruning)
	// 识别所有含有未完成子孙的 parent ID（防止出现子步骤进行中但父步骤已被隐藏的孤儿步骤）
	uncompletedParents := make(map[string]bool)
	for {
		changed := false
		for _, step := range plan.Steps {
			if step.Status != tasklistmodel.StepStatusCompleted && step.ParentID != "" {
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

	var prunedSteps []tasklistmodel.Step
	var completedSteps []tasklistmodel.Step

	for _, step := range plan.Steps {
		if step.Status == tasklistmodel.StepStatusCompleted {
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
		SessionID         string           `json:"session_id"`
		Steps             []tasklistmodel.Step `json:"steps"`
		LatestExplanation string           `json:"latest_explanation,omitempty"`
		Version           int64            `json:"version"`
		ArchivedCompleted int              `json:"archived_completed_count"`
		Message           string           `json:"message,omitempty"`
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
