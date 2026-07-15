package readmcpresource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"genesis-agent/internal/capabilities/mcp/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

// Tool 按需读取 MCP resource。
type Tool struct {
	manager contract.Manager
}

// New 创建 read_mcp_resource 工具。
func New(manager contract.Manager) *Tool {
	return &Tool{manager: manager}
}

func (t *Tool) GetInfo() *tool.Info {
	return tool.WithTraits(&tool.Info{
		Name:        "read_mcp_resource",
		Description: "读取指定 MCP server 上的 resource 内容。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"server": {Type: "string", Description: "MCP server 名称"},
				"uri":    {Type: "string", Description: "resource URI"},
			},
			Required: []string{"server", "uri"},
		},
	}, tool.ToolTraits{
		Exposure:        tool.ToolExposureDirect,
		ReadOnly:        true,
		ConcurrencySafe: true,
		NeedsPermission: true,
	})
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	if t.manager == nil {
		return "", fmt.Errorf("read_mcp_resource: manager 未初始化")
	}
	var req struct {
		Server string `json:"server"`
		URI    string `json:"uri"`
	}
	if err := json.Unmarshal([]byte(params), &req); err != nil {
		return "", fmt.Errorf("参数不是合法 JSON: %w", err)
	}
	req.Server = strings.TrimSpace(req.Server)
	req.URI = strings.TrimSpace(req.URI)
	if req.Server == "" || req.URI == "" {
		return "", fmt.Errorf("server 与 uri 均为必填")
	}
	if err := t.manager.EnsureConnected(ctx, req.Server); err != nil {
		return "", err
	}
	sess, ok := t.manager.SessionFor(req.Server)
	if !ok {
		return "", fmt.Errorf("mcp server %q 未连接", req.Server)
	}
	return sess.ReadResource(ctx, req.URI)
}
