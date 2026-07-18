package tooladapter

import (
	"encoding/json"
	"sort"
	"strings"

	"genesis-agent/internal/capabilities/mcp/model"
)

// DiscoverFileBindings 发现 schema 中明确带文件语义的字段；结果仍需产品首次确认后持久化。
func DiscoverFileBindings(raw json.RawMessage) []model.MCPFileBinding {
	var root map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil {
		return nil
	}
	result := make([]model.MCPFileBinding, 0)
	discoverBindings(root, "", &result)
	sort.Slice(result, func(i, j int) bool { return result[i].JSONPointer < result[j].JSONPointer })
	return result
}

func discoverBindings(node map[string]any, pointer string, result *[]model.MCPFileBinding) {
	if node == nil {
		return
	}
	format, _ := node["format"].(string)
	genesisKind, _ := node["x-genesis-kind"].(string)
	var kind model.MCPFileBindingKind
	switch strings.ToLower(strings.TrimSpace(genesisKind)) {
	case "input_ref":
		kind = model.MCPFileBindingInputRef
	case "artifact":
		kind = model.MCPFileBindingArtifact
	default:
		if format == "uri" || format == "file-path" {
			kind = model.MCPFileBindingInputRef
		}
	}
	if kind != "" && pointer != "" {
		*result = append(*result, model.MCPFileBinding{JSONPointer: pointer, Kind: kind})
	}
	properties, _ := node["properties"].(map[string]any)
	for name, rawChild := range properties {
		child, _ := rawChild.(map[string]any)
		discoverBindings(child, pointer+"/"+escapePointer(name), result)
	}
}

func escapePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
