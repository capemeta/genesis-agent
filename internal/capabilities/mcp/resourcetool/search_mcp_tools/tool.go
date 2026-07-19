package searchmcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tool "genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
)

// Tool 检索 deferred MCP tools，并可提升为 direct 暴露。
type Tool struct {
	registry tool.Registry
}

// New 创建 search_mcp_tools 工具。
func New(registry tool.Registry) *Tool {
	return &Tool{registry: registry}
}

func (t *Tool) GetInfo() *tool.Info {
	return tool.WithTraits(&tool.Info{
		Name:        "search_mcp_tools",
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
		ConcurrencySafe: false, // promote 会改 registry；仅无 promote 时由 AssessConcurrency 升级
		NeedsPermission: true,
	})
}

// AssessConcurrency：无 promote 的检索可并行；promote=true 或解析失败降级串行。
func (t *Tool) AssessConcurrency(_ context.Context, params string) tool.ConcurrencyAssessment {
	// 字段需与 Execute 入参对齐，避免 DisallowUnknownFields 把合法 query/limit 误判为失败。
	var req struct {
		Query   string `json:"query"`
		Promote bool   `json:"promote"`
		Limit   int    `json:"limit"`
	}
	if err := toolparam.DecodeOptional(params, &req); err != nil {
		return tool.ConcurrencyAssessment{}
	}
	if req.Promote {
		return tool.ConcurrencyAssessment{}
	}
	return tool.ConcurrencyAssessment{ConcurrencySafe: true, ReadOnly: true}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	_ = ctx
	if t.registry == nil {
		return "", fmt.Errorf("search_mcp_tools: registry 未初始化")
	}
	var req struct {
		Query   string `json:"query"`
		Promote bool   `json:"promote"`
		Limit   int    `json:"limit"`
	}
	if err := toolparam.DecodeOptional(params, &req); err != nil {
		return "", fmt.Errorf("解析 search_mcp_tools 参数: %w", err)
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
		registered := t.registry.Get(name)
		if registered == nil {
			continue
		}
		info := registered.GetInfo()
		if info == nil || tool.TraitsOf(info).Exposure != tool.ToolExposureDeferred {
			continue
		}
		blob := strings.ToLower(name + " " + info.Description)
		if query != "" && !strings.Contains(blob, query) {
			continue
		}
		promoted := false
		if req.Promote {
			if updater, ok := registered.(tool.ExposureUpdater); ok {
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
