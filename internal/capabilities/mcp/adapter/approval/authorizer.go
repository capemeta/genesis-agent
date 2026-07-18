package approval

import (
	"context"
	"fmt"
	"strings"
	"sync"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/capabilities/mcp/model"
	"genesis-agent/internal/capabilities/tool/gateway"
)

const mcpToolPrefix = "mcp__"

// Authorizer 将 MCP tool / resource 调用接入 approval 能力（ActionMCPCall）。
type Authorizer struct {
	Service approvalcontract.Service

	mu   sync.RWMutex
	defs map[string]model.McpServerDefinition
}

// New 创建 Gateway Authorizer。
func New(svc approvalcontract.Service) *Authorizer {
	return &Authorizer{Service: svc, defs: make(map[string]model.McpServerDefinition)}
}

// SetDefinitions 更新 server 定义（用于 approval_mode）。
func (a *Authorizer) SetDefinitions(defs []model.McpServerDefinition) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.defs = make(map[string]model.McpServerDefinition, len(defs))
	for _, d := range defs {
		a.defs[d.Config.Name] = d
	}
}

func (a *Authorizer) AuthorizeTool(ctx context.Context, request gateway.AuthorizationRequest) (gateway.AuthorizationDecision, error) {
	name := strings.TrimSpace(request.ToolName)
	isMCPTool := strings.HasPrefix(name, mcpToolPrefix)
	isMCPResource := name == "list_mcp_resources" || name == "read_mcp_resource" || name == "search_mcp_tools"
	if !isMCPTool && !isMCPResource {
		return gateway.AuthorizationDecision{Allowed: true}, nil
	}
	if a == nil || a.Service == nil {
		return gateway.AuthorizationDecision{Allowed: true, Reason: "mcp approval service 未配置，默认放行"}, nil
	}
	if !request.Traits.NeedsPermission {
		return gateway.AuthorizationDecision{Allowed: true}, nil
	}

	server, toolName := "", ""
	uri := "mcp://resource/" + name
	if isMCPTool {
		server, toolName = splitMCPName(name)
		uri = fmt.Sprintf("mcp://%s/%s", server, toolName)
		if mode := a.approvalMode(server); mode == model.ApprovalModeAuto {
			return gateway.AuthorizationDecision{Allowed: true, Reason: "mcp approval_mode=auto"}, nil
		}
	}

	decision, err := a.Service.Authorize(ctx, approvalmodel.Request{
		ToolName: name,
		Action:   approvalmodel.ActionMCPCall,
		Resource: approvalmodel.Resource{
			Type:    "mcp_tool",
			URI:     uri,
			Display: name,
			Metadata: map[string]string{
				"server": server,
				"tool":   toolName,
			},
		},
		Reason: "MCP 调用需要审批",
		Risk:   approvalmodel.RiskMedium,
		SuggestedScopes: []approvalmodel.GrantScope{
			approvalmodel.GrantScopeOnce,
			approvalmodel.GrantScopeSession,
		},
	})
	if err != nil {
		return gateway.AuthorizationDecision{}, err
	}
	allowed := decision.Type == approvalmodel.DecisionApproved || decision.Type == approvalmodel.DecisionApprovedForScope
	return gateway.AuthorizationDecision{
		Allowed: allowed,
		Reason:  decision.Reason,
		Metadata: map[string]string{
			"decision": string(decision.Type),
			"scope":    string(decision.Scope),
		},
	}, nil
}

func (a *Authorizer) approvalMode(server string) model.ApprovalMode {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if def, ok := a.defs[server]; ok {
		return def.Config.ApprovalMode
	}
	return ""
}

func splitMCPName(name string) (server, tool string) {
	rest := strings.TrimPrefix(name, mcpToolPrefix)
	parts := strings.SplitN(rest, "__", 2)
	if len(parts) != 2 {
		return rest, ""
	}
	return parts[0], parts[1]
}

var _ gateway.Authorizer = (*Authorizer)(nil)
