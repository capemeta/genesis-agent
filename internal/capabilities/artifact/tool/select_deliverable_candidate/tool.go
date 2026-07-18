package select_deliverable_candidate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
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
		"deliverable_id": {Type: "string", Description: "run_skill_command 返回的 deliverable_id"},
		"candidate_id":   {Type: "string", Description: "同一结果中返回的 candidate_id"},
	}, Required: []string{"deliverable_id", "candidate_id"}}, Traits: toolcontract.ToolTraits{Exposure: toolcontract.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: true, NeedsPermission: true}}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in input
	if err := toolparam.Decode(params, &in); err != nil {
		return "", fmt.Errorf("解析select_deliverable_candidate参数失败: %w", err)
	}
	in.DeliverableID, in.CandidateID = strings.TrimSpace(in.DeliverableID), strings.TrimSpace(in.CandidateID)
	if in.DeliverableID == "" || in.CandidateID == "" {
		return "", fmt.Errorf("deliverable_id与candidate_id不能为空")
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("EXECUTION_BINDING_REQUIRED: 缺少 workspace control plane")
	}
	result, err := t.finalizer.SelectAndFinalize(ctx, prepared.Manifest.Scope.TenantID, prepared.Manifest.RunID, in.DeliverableID, in.CandidateID)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	return string(data), err
}

var _ toolcontract.Tool = (*Tool)(nil)
