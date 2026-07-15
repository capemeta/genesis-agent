package listmcpresources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

// Tool 列出已连接 MCP server 的 resources（不进 LLM schema 时可设 hidden；默认 direct 供按需调用）。
type Tool struct {
	manager contract.Manager
}

// New 创建 list_mcp_resources 工具。
func New(manager contract.Manager) *Tool {
	return &Tool{manager: manager}
}

func (t *Tool) GetInfo() *tool.Info {
	return tool.WithTraits(&tool.Info{
		Name:        "list_mcp_resources",
		Description: "列出已连接 MCP server 的 resources。可按 server 过滤。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"server": {Type: "string", Description: "可选，限定单个 MCP server 名称"},
			},
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
		return "", fmt.Errorf("list_mcp_resources: manager 未初始化")
	}
	var req struct {
		Server string `json:"server"`
	}
	if strings.TrimSpace(params) != "" {
		if err := json.Unmarshal([]byte(params), &req); err != nil {
			return "", fmt.Errorf("参数不是合法 JSON: %w", err)
		}
	}
	type item struct {
		Server      string `json:"server"`
		URI         string `json:"uri"`
		Name        string `json:"name,omitempty"`
		Description string `json:"description,omitempty"`
		MIMEType    string `json:"mime_type,omitempty"`
	}
	out := make([]item, 0)
	req.Server = strings.TrimSpace(req.Server)
	if req.Server != "" {
		if err := t.manager.EnsureConnected(ctx, req.Server); err != nil {
			return "", err
		}
	}
	for _, st := range t.manager.States() {
		if req.Server != "" && st.Name != req.Server {
			continue
		}
		if st.Status == model.ServerStatusStarting {
			_ = t.manager.EnsureConnected(ctx, st.Name)
			st = findState(t.manager.States(), st.Name)
		}
		if st.Status != model.ServerStatusReady {
			continue
		}
		sess, ok := t.manager.SessionFor(st.Name)
		if !ok {
			continue
		}
		resources, err := sess.ListResources(ctx)
		if err != nil {
			continue
		}
		for _, r := range resources {
			out = append(out, item{
				Server:      st.Name,
				URI:         r.URI,
				Name:        r.Name,
				Description: r.Description,
				MIMEType:    r.MIMEType,
			})
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func findState(states []model.ServerState, name string) model.ServerState {
	for _, st := range states {
		if st.Name == name {
			return st
		}
	}
	return model.ServerState{Name: name}
}
