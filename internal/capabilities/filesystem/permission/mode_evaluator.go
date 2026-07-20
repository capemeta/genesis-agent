package permission

import (
	"context"
	"fmt"
	"strings"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
)

// ModeEvaluator 根据 PermissionMode 评估审批请求。
type ModeEvaluator struct{}

// NewModeEvaluator 创建权限模式评估器。
func NewModeEvaluator() *ModeEvaluator {
	return &ModeEvaluator{}
}

// NormalizeMode 规范化输入模式字符串。
func NormalizeMode(raw string) PermissionMode {
	switch PermissionMode(strings.ToLower(strings.TrimSpace(raw))) {
	case PermissionModePlan:
		return PermissionModePlan
	case PermissionModeReadOnly:
		return PermissionModeReadOnly
	case PermissionModeProtectedWrite:
		return PermissionModeProtectedWrite
	case PermissionModeAgent, "default", "workspace_write":
		return PermissionModeAgent
	case PermissionModeFullAccess, "yolo", "bypass", "full-access":
		return PermissionModeFullAccess
	default:
		return PermissionModeAgent
	}
}

// Evaluate 根据 PermissionMode 评估给定的 Request。
func (e *ModeEvaluator) Evaluate(ctx context.Context, mode PermissionMode, req approvalmodel.Request) (approvalmodel.PolicyResult, error) {
	if err := ctx.Err(); err != nil {
		return approvalmodel.PolicyResult{}, err
	}

	normalizedMode := NormalizeMode(string(mode))

	switch normalizedMode {
	case PermissionModePlan:
		return evaluatePlanMode(req), nil
	case PermissionModeReadOnly:
		return evaluateReadOnlyMode(req), nil
	case PermissionModeProtectedWrite:
		return evaluateProtectedWriteMode(req), nil
	case PermissionModeAgent:
		return evaluateAgentMode(req), nil
	case PermissionModeFullAccess:
		return evaluateFullAccessMode(req), nil
	default:
		return evaluateAgentMode(req), nil
	}
}

func isReadRequest(req approvalmodel.Request) bool {
	switch req.Action {
	case approvalmodel.ActionFileRead, approvalmodel.ActionFileList, approvalmodel.ActionFileWalk,
		approvalmodel.ActionSkillResourceRead, approvalmodel.ActionSkillLoad:
		return true
	case approvalmodel.ActionMCPCall:
		meta := mergeMetadata(req)
		if meta["read_only"] == "true" || meta["readOnly"] == "true" {
			return true
		}
		return false
	default:
		return false
	}
}

func isProtectedReadRequest(req approvalmodel.Request) bool {
	if !isReadRequest(req) {
		return false
	}
	meta := mergeMetadata(req)
	return meta["protected"] == "true" || meta["critical"] == "true"
}

func evaluatePlanMode(req approvalmodel.Request) approvalmodel.PolicyResult {
	if isReadRequest(req) {
		if isProtectedReadRequest(req) {
			return approvalmodel.PolicyResult{
				Type:   approvalmodel.PolicyDeny,
				Reason: "plan 模式下强制禁止读取受保护/机密凭证路径",
				Risk:   approvalmodel.RiskCritical,
			}
		}
		return approvalmodel.PolicyResult{
			Type:   approvalmodel.PolicyAllow,
			Reason: "plan 模式放行只读与探索操作",
			Risk:   approvalmodel.RiskLow,
		}
	}
	if req.Action == approvalmodel.ActionPlanExitApprove {
		return approvalmodel.PolicyResult{
			Type:            approvalmodel.PolicyAsk,
			Reason:          "退出 plan 模式需要人工确认",
			Risk:            approvalmodel.RiskMedium,
			SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce},
		}
	}
	return approvalmodel.PolicyResult{
		Type:   approvalmodel.PolicyDeny,
		Reason: fmt.Sprintf("plan 模式强制禁止修改与命令执行操作 (%s)", req.Action),
		Risk:   approvalmodel.RiskCritical,
	}
}

func evaluateReadOnlyMode(req approvalmodel.Request) approvalmodel.PolicyResult {
	if isReadRequest(req) {
		if isProtectedReadRequest(req) {
			return approvalmodel.PolicyResult{
				Type:   approvalmodel.PolicyAsk,
				Reason: "read_only 模式下读取受保护/机密凭证路径需人工审批确认",
				Risk:   approvalmodel.RiskHigh,
				SuggestedScopes: []approvalmodel.GrantScope{
					approvalmodel.GrantScopeOnce,
					approvalmodel.GrantScopeSession,
					approvalmodel.GrantScopeProject,
				},
			}
		}
		return approvalmodel.PolicyResult{
			Type:   approvalmodel.PolicyAllow,
			Reason: "read_only 模式放行普通只读操作",
			Risk:   approvalmodel.RiskLow,
		}
	}
	return approvalmodel.PolicyResult{
		Type:   approvalmodel.PolicyDeny,
		Reason: fmt.Sprintf("read_only 纯只读模式硬拦截修改、命令与网络操作 (%s)", req.Action),
		Risk:   approvalmodel.RiskHigh,
	}
}

func evaluateProtectedWriteMode(req approvalmodel.Request) approvalmodel.PolicyResult {
	if isReadRequest(req) {
		if isProtectedReadRequest(req) {
			return approvalmodel.PolicyResult{
				Type:   approvalmodel.PolicyAsk,
				Reason: "protected_write 模式下读取受保护/机密凭证路径需人工审批确认",
				Risk:   approvalmodel.RiskHigh,
				SuggestedScopes: []approvalmodel.GrantScope{
					approvalmodel.GrantScopeOnce,
					approvalmodel.GrantScopeSession,
					approvalmodel.GrantScopeProject,
				},
			}
		}
		return approvalmodel.PolicyResult{
			Type:   approvalmodel.PolicyAllow,
			Reason: "protected_write 模式放行普通只读操作",
			Risk:   approvalmodel.RiskLow,
		}
	}

	meta := mergeMetadata(req)
	// 只有 full_access 模式能触发敏感/受保护路径的编辑确认，其他模式硬性强制禁止编辑敏感路径
	if meta["protected"] == "true" || meta["critical"] == "true" {
		return approvalmodel.PolicyResult{
			Type:   approvalmodel.PolicyDeny,
			Reason: fmt.Sprintf("非 full_access 模式硬性强制禁止修改受保护/敏感路径 (%s)", firstNonEmpty(meta["deny_reason"], "受保护路径禁止修改")),
			Risk:   approvalmodel.RiskCritical,
		}
	}

	return approvalmodel.PolicyResult{
		Type:   approvalmodel.PolicyAsk,
		Reason: fmt.Sprintf("protected_write 受控写模式：修改操作 (%s) 需人工审批", req.Action),
		Risk:   approvalmodel.RiskHigh,
		SuggestedScopes: []approvalmodel.GrantScope{
			approvalmodel.GrantScopeOnce,
			approvalmodel.GrantScopeSession,
			approvalmodel.GrantScopeProject,
		},
	}
}

func evaluateAgentMode(req approvalmodel.Request) approvalmodel.PolicyResult {
	if isReadRequest(req) {
		if isProtectedReadRequest(req) {
			return approvalmodel.PolicyResult{
				Type:   approvalmodel.PolicyAsk,
				Reason: "agent 模式下读取受保护/机密凭证路径需人工审批确认",
				Risk:   approvalmodel.RiskHigh,
				SuggestedScopes: []approvalmodel.GrantScope{
					approvalmodel.GrantScopeOnce,
					approvalmodel.GrantScopeSession,
					approvalmodel.GrantScopeProject,
				},
			}
		}
		return approvalmodel.PolicyResult{
			Type:   approvalmodel.PolicyAllow,
			Reason: "agent 模式放行普通只读操作",
			Risk:   approvalmodel.RiskLow,
		}
	}

	meta := mergeMetadata(req)
	// 只有 full_access 模式能触发敏感/受保护路径的编辑确认，其他模式硬性强制禁止编辑敏感路径
	if meta["protected"] == "true" || meta["critical"] == "true" {
		return approvalmodel.PolicyResult{
			Type:   approvalmodel.PolicyDeny,
			Reason: fmt.Sprintf("非 full_access 模式硬性强制禁止修改受保护/敏感路径 (%s)", firstNonEmpty(meta["deny_reason"], "受保护路径禁止修改")),
			Risk:   approvalmodel.RiskCritical,
		}
	}

	if req.Action == approvalmodel.ActionFileWrite || req.Action == approvalmodel.ActionFileEdit {
		scope := getScopeMetadata(req)
		if scope == "external" {
			return approvalmodel.PolicyResult{
				Type:   approvalmodel.PolicyAsk,
				Reason: "agent 模式写工作区外文件需要审批",
				Risk:   approvalmodel.RiskHigh,
				SuggestedScopes: []approvalmodel.GrantScope{
					approvalmodel.GrantScopeOnce,
					approvalmodel.GrantScopeSession,
					approvalmodel.GrantScopeProject,
				},
			}
		}
		return approvalmodel.PolicyResult{
			Type:   approvalmodel.PolicyAllow,
			Reason: "agent 模式放行工作区写操作",
			Risk:   approvalmodel.RiskLow,
		}
	}
	// 命令、HTTP请求、MCP调用、Skill安装等需要审批
	return approvalmodel.PolicyResult{
		Type:   approvalmodel.PolicyAsk,
		Reason: fmt.Sprintf("agent 模式下操作 (%s) 需要人工审批", req.Action),
		Risk:   approvalmodel.RiskHigh,
		SuggestedScopes: []approvalmodel.GrantScope{
			approvalmodel.GrantScopeOnce,
			approvalmodel.GrantScopeSession,
			approvalmodel.GrantScopeProject,
		},
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func evaluateFullAccessMode(req approvalmodel.Request) approvalmodel.PolicyResult {
	if isReadRequest(req) {
		return approvalmodel.PolicyResult{
			Type:   approvalmodel.PolicyAllow,
			Reason: "full_access 模式自动放行只读操作",
			Risk:   approvalmodel.RiskLow,
		}
	}

	meta := mergeMetadata(req)
	// 即使在 Full-Access 模式下，系统致命级核心修改（如 system32, SAM, sudoers, TCC.db）依然硬拒绝！
	if meta["critical"] == "true" {
		return approvalmodel.PolicyResult{
			Type:   approvalmodel.PolicyDeny,
			Reason: fmt.Sprintf("full_access 模式下依然硬拒绝篡改系统致命核心文件/注册表/关键配置 (%s)", meta["deny_reason"]),
			Risk:   approvalmodel.RiskCritical,
		}
	}

	// 凭证、秘钥与启动项写操作在 Full-Access 模式下仍触发人工确认
	if meta["protected"] == "true" && (req.Action == approvalmodel.ActionFileWrite || req.Action == approvalmodel.ActionFileEdit || req.Action == approvalmodel.Action("file.delete")) {
		return approvalmodel.PolicyResult{
			Type:   approvalmodel.PolicyAsk,
			Reason: "full_access 模式修改高危凭证/私钥/启动项配置需人工显式确认",
			Risk:   approvalmodel.RiskHigh,
			SuggestedScopes: []approvalmodel.GrantScope{
				approvalmodel.GrantScopeOnce,
				approvalmodel.GrantScopeSession,
				approvalmodel.GrantScopeProject,
			},
		}
	}

	return approvalmodel.PolicyResult{
		Type:   approvalmodel.PolicyAllow,
		Reason: "full_access 模式放行常规修改与命令",
		Risk:   approvalmodel.RiskLow,
	}
}

func getScopeMetadata(req approvalmodel.Request) string {
	if req.Metadata != nil {
		if s := req.Metadata["scope"]; s != "" {
			return s
		}
	}
	if req.Resource.Metadata != nil {
		if s := req.Resource.Metadata["scope"]; s != "" {
			return s
		}
	}
	return "workspace"
}

func mergeMetadata(req approvalmodel.Request) map[string]string {
	out := make(map[string]string)
	for k, v := range req.Resource.Metadata {
		out[k] = v
	}
	for k, v := range req.Metadata {
		out[k] = v
	}
	return out
}

