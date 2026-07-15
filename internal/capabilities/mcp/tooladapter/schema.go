package tooladapter

import (
	"encoding/json"

	tool "genesis-agent/internal/capabilities/tool/contract"
)

// ConvertInputSchema 将 MCP inputSchema 转为 tool.ParameterSchema。
func ConvertInputSchema(raw json.RawMessage) *tool.ParameterSchema {
	if len(raw) == 0 {
		return &tool.ParameterSchema{Type: "object", Properties: map[string]*tool.ParameterSchema{}}
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return &tool.ParameterSchema{Type: "object", Properties: map[string]*tool.ParameterSchema{}}
	}
	return convertNode(generic)
}

func convertNode(node map[string]any) *tool.ParameterSchema {
	if node == nil {
		return &tool.ParameterSchema{Type: "object"}
	}
	out := &tool.ParameterSchema{}
	if t, ok := node["type"].(string); ok {
		out.Type = t
	}
	if d, ok := node["description"].(string); ok {
		out.Description = d
	}
	if props, ok := node["properties"].(map[string]any); ok {
		out.Properties = make(map[string]*tool.ParameterSchema, len(props))
		for k, v := range props {
			if child, ok := v.(map[string]any); ok {
				out.Properties[k] = convertNode(child)
			}
		}
	}
	if req, ok := node["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				out.Required = append(out.Required, s)
			}
		}
	}
	if enum, ok := node["enum"].([]any); ok {
		for _, e := range enum {
			if s, ok := e.(string); ok {
				out.Enum = append(out.Enum, s)
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		out.Items = convertNode(items)
	}
	if out.Type == "" {
		out.Type = "object"
	}
	return out
}
