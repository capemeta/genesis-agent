package builtin

import (
	"context"
	"fmt"
	"strings"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	planmodeprompt "genesis-agent/internal/capabilities/planmode/prompt"
	"genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/collab"
	multicontract "genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/progress"
)

// ExitPlanModeTool 请求用户批准并退出规划模式。
type ExitPlanModeTool struct {
	approval approvalcontract.Service
	docs     collab.PlanDocuments
}

// NewExitPlanModeTool 创建工具；approval 用于方案期 HiTL；docs 用于校验方案文件。
func NewExitPlanModeTool(approval approvalcontract.Service, docs collab.PlanDocuments) tool.Tool {
	return &ExitPlanModeTool{approval: approval, docs: docs}
}

func (t *ExitPlanModeTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "exit_plan_mode",
		Description: planmodeprompt.ToolExitPlanModeDescription,
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"summary": {
					Type:        "string",
					Description: "实施方案摘要（给用户审批用，1–5 句）",
				},
			},
			Required: []string{},
		},
		Traits: tool.ToolTraits{
			Exposure:        tool.ToolExposureDirect,
			ReadOnly:        false,
			ConcurrencySafe: false,
			NeedsPermission: true,
		},
	}
}

func (t *ExitPlanModeTool) Execute(ctx context.Context, params string) (string, error) {
	var args struct {
		Summary string `json:"summary"`
	}
	if err := toolparam.DecodeOptional(params, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	if multicontract.DelegationDepth(ctx) > 0 {
		return "", fmt.Errorf("子智能体禁止退出规划模式")
	}
	sessionID, ok := contextutil.GetSessionID(ctx)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("context missing session_id")
	}
	mode, err := collab.SessionMode(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if mode != collab.ModePlan {
		return "", fmt.Errorf("当前不在规划模式，无法退出")
	}
	store, ok := collab.StoreFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("协作模式 Store 未注入")
	}
	if t.docs == nil {
		return "", fmt.Errorf("实施方案存储未注入")
	}
	planPath, body, err := t.docs.Read(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("读取实施方案失败: %w", err)
	}
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("尚未写入实施方案（期望路径 %s）。请先调用 write_implementation_plan", planPath)
	}
	if t.approval == nil {
		return "", fmt.Errorf("退出规划模式需要审批服务")
	}
	reason := "请求批准退出规划模式并开始执行"
	if args.Summary != "" {
		reason = args.Summary
	}
	decision, err := t.approval.Authorize(ctx, approvalmodel.Request{
		ToolName: "exit_plan_mode",
		Action:   approvalmodel.ActionPlanExitApprove,
		Resource: approvalmodel.Resource{
			Type:    "implementation_plan",
			URI:     "workspace://" + planPath,
			Display: "实施方案",
		},
		Reason: reason,
		Risk:   approvalmodel.RiskHigh,
		SuggestedScopes: []approvalmodel.GrantScope{
			approvalmodel.GrantScopeOnce,
		},
	})
	if err != nil {
		return "", fmt.Errorf("退出规划模式审批失败: %w", err)
	}
	switch decision.Type {
	case approvalmodel.DecisionApproved, approvalmodel.DecisionApprovedForScope:
		if err := store.Set(ctx, sessionID, collab.SessionState{Mode: collab.ModeDefault, HandoffPending: true}); err != nil {
			return "", fmt.Errorf("保存退出状态失败: %w", err)
		}
		progress.Emit(ctx, progress.Event{
			Kind:    progress.KindCollaborationMode,
			Phase:   progress.PhaseComplete,
			Summary: "已退出规划模式（方案已批准）",
			Detail:  string(collab.ModeDefault),
			Name:    "default",
		})
		return fmt.Sprintf(
			"用户已批准退出规划模式。实施方案位于 %s。请先读取该方案（若需要），再用 todo_write 拆解任务清单，然后执行。不要跳过清单直接改代码。",
			planPath,
		), nil
	case approvalmodel.DecisionDenied, approvalmodel.DecisionAbort, approvalmodel.DecisionTimedOut:
		return planmodeprompt.RejectReminder(planPath), nil
	default:
		return planmodeprompt.RejectReminder(planPath), nil
	}
}
