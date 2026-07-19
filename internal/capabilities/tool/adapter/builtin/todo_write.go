package builtin

import (
	"context"
	"fmt"

	tasklistcontract "genesis-agent/internal/capabilities/tasklist/contract"
	tasklistmodel "genesis-agent/internal/capabilities/tasklist/model"
	tasklistprompt "genesis-agent/internal/capabilities/tasklist/prompt"
	"genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	"genesis-agent/internal/platform/contextutil"
)

// TodoWriteTool 全量更新待办计划的工具
type TodoWriteTool struct {
	planSvc tasklistcontract.Service
}

// NewTodoWriteTool 创建 TodoWriteTool 实例
func NewTodoWriteTool(svc tasklistcontract.Service) tool.Tool {
	return &TodoWriteTool{planSvc: svc}
}

func (t *TodoWriteTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "todo_write",
		Description: tasklistprompt.ToolTodoWriteDescription,
		Parameters: &tool.ParameterSchema{
			Type:     "object",
			Required: []string{"steps"},
			Properties: map[string]*tool.ParameterSchema{
				"steps": {
					Type:        "array",
					Description: "新的完整待办事项步骤数组。会全量覆盖旧列表。",
					Items: &tool.ParameterSchema{
						Type:     "object",
						Required: []string{"title", "status"},
						Properties: map[string]*tool.ParameterSchema{
							"title":    {Type: "string", Description: "步骤文字描述（请精炼在 5-10 个字内，如 '分析配置文件'）"},
							"status":   {Type: "string", Enum: []string{"pending", "in_progress", "completed"}},
							"priority": {Type: "string", Enum: []string{"low", "medium", "high"}},
							"assignee": {Type: "string", Description: "指定执行人，如 'agent' 或 'user'（可选）"},
							"notes":    {Type: "string", Description: "额外说明或备注，记录步骤潜在依赖（可选）"},
						},
					},
				},
				"explanation": {
					Type:        "string",
					Description: "说明本次全量重构或规划的核心原因，用以写入审计历史。",
				},
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

func (t *TodoWriteTool) Execute(ctx context.Context, params string) (string, error) {
	sessionID, ok := contextutil.GetSessionID(ctx)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("context missing session_id")
	}

	var args struct {
		Steps       []tasklistmodel.Step `json:"steps"`
		Explanation string           `json:"explanation"`
	}

	if err := toolparam.Decode(params, &args); err != nil {
		return "", fmt.Errorf("unmarshal parameters failed: %w", err)
	}

	plan, err := t.planSvc.UpdateTaskList(ctx, sessionID, args.Steps, args.Explanation, "agent")
	if err != nil {
		return "", fmt.Errorf("update plan failed: %w", err)
	}

	// 检查是否有步骤因重构触发拦截审批
	hasBlocked := false
	for _, step := range plan.Steps {
		if step.Status == tasklistmodel.StepStatusBlockedByApproval {
			hasBlocked = true
			break
		}
	}

	if hasBlocked {
		return fmt.Sprintf("任务清单覆写已成功提交。检测到属于【重大结构重构】，状态已锁定为 blocked_by_approval。系统已自动弹起人机审批流，请告知用户核准后再行执行。最新版本号：%d", plan.Version), nil
	}

	return fmt.Sprintf("任务清单已成功覆写。最新版本号：%d，当前步骤均已准备完毕。", plan.Version), nil
}
