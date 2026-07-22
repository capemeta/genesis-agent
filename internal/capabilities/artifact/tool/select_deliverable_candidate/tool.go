package select_deliverable_candidate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactservice "genesis-agent/internal/capabilities/artifact/service"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

type Tool struct {
	finalizer artifactcontract.RequiredDeliverableFinalizer
}
type input struct {
	DeliverableID string `json:"deliverable_id"`
	CandidateID   string `json:"candidate_id"`
}

func New(finalizer artifactcontract.RequiredDeliverableFinalizer) (toolcontract.Tool, error) {
	if finalizer == nil {
		return nil, fmt.Errorf("deliverable finalizer未配置")
	}
	return &Tool{finalizer: finalizer}, nil
}

func (t *Tool) GetInfo() *toolcontract.Info {
	return &toolcontract.Info{Name: "select_deliverable_candidate", Description: "当 Harness 明确返回多个交付候选时，从返回集合中选择一个不透明 candidate_id；不得提交路径、文件名或 locator。选择后由 Harness 自动完成 Gate、发布与交付。", Parameters: &toolcontract.ParameterSchema{Type: "object", Properties: map[string]*toolcontract.ParameterSchema{
		"deliverable_id": {Type: "string", Description: "可选。待确认交付物的 ID；如未提供，Harness 会根据 candidate_id 自动推导匹配"},
		"candidate_id":   {Type: "string", Description: "来自 TaskResult 或产物列表中的 candidate_id（形如 produced-xxx）。禁止使用 result_id、run_id 或文件路径"},
	}, Required: []string{"candidate_id"}}, Traits: toolcontract.ToolTraits{Exposure: toolcontract.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: true, NeedsPermission: true}}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolparam.Decode(params, &in); err != nil {
		return "", fmt.Errorf("解析select_deliverable_candidate参数失败: %w", err)
	}
	in.DeliverableID, in.CandidateID = strings.TrimSpace(in.DeliverableID), strings.TrimSpace(in.CandidateID)
	if in.CandidateID == "" {
		return "", fmt.Errorf("candidate_id不能为空")
	}
	if strings.HasPrefix(in.CandidateID, "result-") || strings.HasPrefix(in.CandidateID, "run-") || strings.HasPrefix(in.CandidateID, "agent-") {
		return "", fmt.Errorf("INVALID_CANDIDATE_ID: %q 是任务/运行 ID，不是产物 ID。请在 TaskResult.artifacts 列表中找到形如 produced-xxx 的 candidate_id 重新提交", in.CandidateID)
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("EXECUTION_BINDING_REQUIRED: 缺少 workspace control plane")
	}
	// 跨 Run 候选（子 Run 产物）已被父在 finish 时接纳为「只读引用」，其交付在子 Run 内完成：
	// 父不得再在自己作用域 select/发布子产物。返回明确的边界码，不伪装成 NOT_FOUND（spec §7.2）。
	if rec, found := artifactservice.GlobalAdoptionStore.Resolve(prepared.Manifest.Scope.TenantID, prepared.Manifest.RunID, in.CandidateID, prepared.Manifest.StateRoot.Path); found && rec.OwnerRunID != "" && rec.OwnerRunID != prepared.Manifest.RunID {
		return "", fmt.Errorf("ADOPTION_REQUIRED: candidate %q 由子 Run %q 生产、已被当前 Run 接纳为只读引用；其交付已在子 Run 内完成。父 Agent 应直接向用户总结已交付结果，不要在父 Run 重新 select/发布子产物", in.CandidateID, rec.OwnerRunID)
	}
	result, err := t.finalizer.SelectAndFinalize(ctx, prepared.Manifest.Scope.TenantID, prepared.Manifest.RunID, in.DeliverableID, in.CandidateID)
	if err != nil {
		return "", fmt.Errorf("PRODUCED_RESOURCE_NOT_FOUND: 未找到 ID 为 %q 的产物。请检查 candidate_id 是否正确（应形如 produced-xxx）: %w", in.CandidateID, err)
	}
	data, err := json.Marshal(result)
	return string(data), err
}

var _ toolcontract.Tool = (*Tool)(nil)
