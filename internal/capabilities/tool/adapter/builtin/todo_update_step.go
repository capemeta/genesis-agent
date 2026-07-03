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

// TodoUpdateStepTool 差量更新步骤状态的工具
type TodoUpdateStepTool struct {
	planSvc plancontract.Service
}

// NewTodoUpdateStepTool 创建 TodoUpdateStepTool 实例
func NewTodoUpdateStepTool(svc plancontract.Service) tool.Tool {
	return &TodoUpdateStepTool{planSvc: svc}
}

func (t *TodoUpdateStepTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "todo_update_step",
		Description: "快速差量滚动单个步骤的状态。当您完成当前步骤或启动下一个步骤时，必须首选调用此工具（出站 Token 最小、延迟极低）。",
		Parameters: &tool.ParameterSchema{
			Type:     "object",
			Required: []string{"id", "status"},
			Properties: map[string]*tool.ParameterSchema{
				"id":          {Type: "string", Description: "需要变更状态的步骤唯一 ID（可通过 todo_read 获得）"},
				"status":      {Type: "string", Enum: []string{"pending", "in_progress", "completed"}},
				"explanation": {Type: "string", Description: "说明本次单点进度滚动的核心动作（可选，如 '完成 API 连通测试'）"},
			},
		},
		Traits: tool.ToolTraits{
			Exposure:        tool.ToolExposureDirect,
			ReadOnly:        false,
			ConcurrencySafe: false,
			NeedsPermission: true,
		},
	}
}

func (t *TodoUpdateStepTool) Execute(ctx context.Context, params string) (string, error) {
	sessionID, ok := contextutil.GetSessionID(ctx)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("context missing session_id")
	}

	var args struct {
		ID          string               `json:"id"`
		Status      planmodel.StepStatus `json:"status"`
		Explanation string               `json:"explanation"`
	}

	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return "", fmt.Errorf("unmarshal parameters failed: %w", err)
	}

	plan, err := t.planSvc.UpdateStepStatus(ctx, sessionID, args.ID, args.Status, args.Explanation, "agent")
	if err != nil {
		return "", fmt.Errorf("update step status failed: %w", err)
	}

	return fmt.Sprintf("步骤 [%s] 已成功流转为 %s。最新版本号：%d。", args.ID, args.Status, plan.Version), nil
}
