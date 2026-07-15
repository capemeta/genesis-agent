package mcpsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tool "genesis-agent/internal/capabilities/tool/contract"
)

// Tool 检索 deferred MCP tools，并可提升为 direct 暴露。
type Tool struct {
	registry tool.Registry
}

// New 创建 mcp_search 工具。
func New(registry tool.Registry) *Tool {
	return &Tool{registry: registry}
}

func (t *Tool) GetInfo() *tool.Info {
	return tool.WithTraits(&tool.Info{
		Name:        "mcp_search",
		Description: "搜索已注册但 deferred 暴露的 MCP 工具（mcp__*）；promote=true 时提升为 direct 进入 LLM schema。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"query":   {Type: "string", Description: "关键词，匹配工具名或描述"},
				"promote": {Type: "boolean", Description: "为 true 时把匹配的 deferred 工具提升为 direct"},
				"limit":   {Type: "integer", Description: "返回条数上限，默认 20"},
			},
		},
	}, tool.ToolTraits{
		Exposure:        tool.ToolExposureDirect,
		ReadOnly:        false,
		ConcurrencySafe: true,
		NeedsPermission: true,
	})
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	_ = ctx
	if t.registry == nil {
		return "", fmt.Errorf("mcp_search: registry 未初始化")
	}
	var req struct {
		Query   string `json:"query"`
		Promote bool   `json:"promote"`
		Limit   int    `json:"limit"`
	}
	if strings.TrimSpace(params) != "" {
		if err := json.Unmarshal([]byte(params), &req); err != nil {
			return "", fmt.Errorf("参数不是合法 JSON: %w", err)
		}
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	query := strings.ToLower(strings.TrimSpace(req.Query))

	type hit struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Promoted    bool   `json:"promoted,omitempty"`
	}
	hits := make([]hit, 0)
	for _, name := range t.registry.Names() {
		if !strings.HasPrefix(name, "mcp__") {
			continue
		}
		tr := t.registry.Get(name)
		if tr == nil {
			continue
		}
		info := tr.GetInfo()
		if info == nil {
			continue
		}
		if tool.TraitsOf(info).Exposure != tool.ToolExposureDeferred {
			continue
		}
		blob := strings.ToLower(name + " " + info.Description)
		if query != "" && !strings.Contains(blob, query) {
			continue
		}
		promoted := false
		if req.Promote {
			if updater, ok := tr.(tool.ExposureUpdater); ok {
				updater.SetExposure(tool.ToolExposureDirect)
				promoted = true
			}
		}
		hits = append(hits, hit{Name: name, Description: info.Description, Promoted: promoted})
		if len(hits) >= req.Limit {
			break
		}
	}
	raw, err := json.Marshal(hits)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
