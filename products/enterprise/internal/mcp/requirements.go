package mcp

import (
	"context"
	"strings"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
)

// StdioDenyFilter 企业默认禁止本地 stdio MCP server（对齐 Codex mcp_requirements）。
type StdioDenyFilter struct{}

// Filter 强制禁用不合规 server。
func (StdioDenyFilter) Filter(ctx context.Context, defs []model.McpServerDefinition) ([]model.McpServerDefinition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]model.McpServerDefinition, len(defs))
	copy(out, defs)
	for i := range out {
		if out[i].Config.Type == model.McpTransportStdio || out[i].Config.Type == "" {
			out[i].Config.Enabled = false
			if strings.TrimSpace(out[i].DisabledReason) == "" {
				out[i].DisabledReason = "enterprise 默认禁止本地 stdio MCP server"
			}
		}
	}
	return out, nil
}

var _ contract.RequirementsFilter = StdioDenyFilter{}
