package builtin

import (
	"context"
	"fmt"
	"strings"

	planmodeprompt "genesis-agent/internal/capabilities/planmode/prompt"
	"genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/collab"
	multicontract "genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/progress"
)

// EnterPlanModeTool 主会话进入规划模式。
type EnterPlanModeTool struct {
	docs collab.PlanDocuments
}

// NewEnterPlanModeTool 创建工具；docs 可选，用于再进入时提示已有方案。
func NewEnterPlanModeTool(docs collab.PlanDocuments) tool.Tool {
	return &EnterPlanModeTool{docs: docs}
}

func (t *EnterPlanModeTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "enter_plan_mode",
		Description: planmodeprompt.ToolEnterPlanModeDescription,
		Parameters: &tool.ParameterSchema{
			Type:       "object",
			Properties: map[string]*tool.ParameterSchema{},
			Required:   []string{},
		},
		Traits: tool.ToolTraits{
			Exposure:        tool.ToolExposureDirect,
			ReadOnly:        false,
			ConcurrencySafe: false,
			NeedsPermission: false,
		},
	}
}

func (t *EnterPlanModeTool) Execute(ctx context.Context, params string) (string, error) {
	if err := toolparam.DecodeOptional(params, &struct{}{}); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	if multicontract.DelegationDepth(ctx) > 0 {
		return "", fmt.Errorf("子智能体禁止进入规划模式")
	}
	sessionID, ok := contextutil.GetSessionID(ctx)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("context missing session_id")
	}
	store, ok := collab.StoreFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("协作模式 Store 未注入")
	}
	if err := store.Set(ctx, sessionID, collab.SessionState{Mode: collab.ModePlan, HandoffPending: false}); err != nil {
		return "", fmt.Errorf("进入规划模式失败: %w", err)
	}
	planPath := collab.PlanDocumentRelPath(sessionID)
	planExists := false
	if t.docs != nil {
		if _, body, err := t.docs.Read(ctx, sessionID); err == nil && strings.TrimSpace(body) != "" {
			planExists = true
		}
	}
	progress.Emit(ctx, progress.Event{
		Kind:    progress.KindCollaborationMode,
		Phase:   progress.PhaseComplete,
		Summary: "已进入规划模式",
		Detail:  string(collab.ModePlan),
		Name:    "plan_mode",
	})
	return planmodeprompt.EnterAck(planPath, planExists), nil
}
