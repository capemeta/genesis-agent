// Package collision 识别「把 Skill 名当成 Tool 名调用」的协议错误，并可同轮改写为 Skill 网关调用。
package collision

import (
	"context"
	"encoding/json"
	"strings"

	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
)

type Cataloger interface {
	Catalog(context.Context, skillcontract.CatalogRequest) (skillmodel.Catalog, error)
}

// Result 是结构化纠错结果，作为 ToolResult 返回给模型（仅在不改写时使用）。
type Result struct {
	Type          string            `json:"type"`
	Requested     string            `json:"requested"`
	Message       string            `json:"message"`
	SuggestedTool string            `json:"suggested_tool"`
	SuggestedArgs map[string]string `json:"suggested_args"`
	Rewritten     bool              `json:"rewritten,omitempty"`
}

// Matcher 用本 turn 的 Skill Catalog 判断名称是否为 skill。
type Matcher struct {
	Service        Cataloger
	CatalogRequest skillcontract.CatalogRequest
}

// Match 若 name 命中 catalog 中的 skill，返回规范名；否则 ok=false。
func (m *Matcher) Match(ctx context.Context, name string) (canonical string, ok bool, err error) {
	if m == nil || m.Service == nil {
		return "", false, nil
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "Skill" {
		return "", false, nil
	}
	catalog, err := m.Service.Catalog(ctx, m.CatalogRequest)
	if err != nil {
		return "", false, err
	}
	for _, entry := range catalog.Entries {
		if entry.Name == name || entry.QualifiedName == name {
			return entry.QualifiedName, true, nil
		}
	}
	return "", false, nil
}

// FormatResult 生成 skill_tool_collision JSON（不改写路径）。
func FormatResult(requested, canonical string) string {
	if canonical == "" {
		canonical = requested
	}
	payload := Result{
		Type:          "skill_tool_collision",
		Requested:     requested,
		Message:       requested + ` 是 Skill，不是 Tool。请调用 Skill(skill="` + canonical + `")。`,
		SuggestedTool: "Skill",
		SuggestedArgs: map[string]string{"skill": canonical},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"type":"skill_tool_collision","message":"skill name used as tool"}`
	}
	return string(data)
}

// RewriteArgs 将误调用改写为 Skill 网关参数。
// 默认丢弃伪造业务 JSON，仅保留 skill 名；若原参数是纯字符串则作为显式 task。
func RewriteArgs(canonical, originalArgs string) string {
	canonical = strings.TrimSpace(canonical)
	payload := map[string]string{"skill": canonical}
	trimmed := strings.TrimSpace(originalArgs)
	if trimmed != "" && trimmed[0] != '{' && trimmed[0] != '[' {
		payload["task"] = trimmed
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"skill":"` + canonical + `"}`
	}
	return string(data)
}
