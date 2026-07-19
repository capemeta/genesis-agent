package builtin

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	planmodeprompt "genesis-agent/internal/capabilities/planmode/prompt"
	"genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/collab"
	"genesis-agent/internal/runtime/progress"
)

// WriteImplementationPlanTool 写入会话实施方案文件（规划模式唯一可写通道）。
type WriteImplementationPlanTool struct {
	docs collab.PlanDocuments
}

// NewWriteImplementationPlanTool 创建工具；docs 由产品 bootstrap 注入。
func NewWriteImplementationPlanTool(docs collab.PlanDocuments) tool.Tool {
	return &WriteImplementationPlanTool{docs: docs}
}

func (t *WriteImplementationPlanTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "write_implementation_plan",
		Description: planmodeprompt.ToolWriteImplementationPlanDescription,
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"content": {
					Type:        "string",
					Description: "实施方案全文（Markdown）",
				},
			},
			Required: []string{"content"},
		},
		Traits: tool.ToolTraits{
			Exposure:        tool.ToolExposureDirect,
			ReadOnly:        false,
			ConcurrencySafe: false,
			NeedsPermission: false,
		},
	}
}

func (t *WriteImplementationPlanTool) Execute(ctx context.Context, params string) (string, error) {
	var args struct {
		Content string `json:"content"`
	}
	if err := toolparam.Decode(params, &args); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
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
		return "", fmt.Errorf("write_implementation_plan 仅在规划模式下可用")
	}
	if t.docs == nil {
		return "", fmt.Errorf("实施方案存储未注入")
	}
	if strings.TrimSpace(args.Content) == "" {
		return "", fmt.Errorf("content 不能为空")
	}
	rel, err := t.docs.Write(ctx, sessionID, args.Content)
	if err != nil {
		return "", err
	}
	progress.Emit(ctx, progress.Event{
		Kind:    progress.KindPlanDocument,
		Phase:   progress.PhaseComplete,
		Summary: "已更新实施方案",
		Detail:  rel,
		Name:    "implementation_plan",
	})
	n := utf8.RuneCountInString(args.Content)
	return fmt.Sprintf("实施方案已写入 %s（约 %d 字）。可用 exit_plan_mode 请求用户批准。", rel, n), nil
}
