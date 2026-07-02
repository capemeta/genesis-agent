// Package eino - tool.Info → eino schema.ToolInfo 转换
// 工具元信息的 eino 格式化只在 llm/eino 包内发生，engine 和 tool 包对 eino 无感知
package eino

import (
	"genesis-agent/internal/capabilities/tool/contract"

	"github.com/cloudwego/eino/schema"
)

// toolInfosToSchema 批量将 tool.Info 转换为 eino schema.ToolInfo
func toolInfosToSchema(infos []*tool.Info) []*schema.ToolInfo {
	result := make([]*schema.ToolInfo, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		result = append(result, toolInfoToSchema(info))
	}
	return result
}

// toolInfoToSchema 将单个 tool.Info 转换为 eino schema.ToolInfo
func toolInfoToSchema(info *tool.Info) *schema.ToolInfo {
	ti := &schema.ToolInfo{
		Name: info.Name,
		Desc: info.Description,
	}
	if info.Parameters != nil && len(info.Parameters.Properties) > 0 {
		ti.ParamsOneOf = schema.NewParamsOneOfByParams(convertProperties(info.Parameters))
	} else {
		ti.ParamsOneOf = schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})
	}
	return ti
}

// convertProperties 将 tool.ParameterSchema.Properties 转换为 eino ParameterInfo map
func convertProperties(ps *tool.ParameterSchema) map[string]*schema.ParameterInfo {
	if ps == nil || len(ps.Properties) == 0 {
		return map[string]*schema.ParameterInfo{}
	}
	requiredSet := make(map[string]bool, len(ps.Required))
	for _, r := range ps.Required {
		requiredSet[r] = true
	}
	result := make(map[string]*schema.ParameterInfo, len(ps.Properties))
	for name, prop := range ps.Properties {
		result[name] = convertParam(prop, requiredSet[name])
	}
	return result
}

// convertParam 将单个 ParameterSchema 转换为 eino ParameterInfo
func convertParam(ps *tool.ParameterSchema, required bool) *schema.ParameterInfo {
	if ps == nil {
		return &schema.ParameterInfo{Type: schema.String}
	}
	info := &schema.ParameterInfo{
		Type:     schema.DataType(ps.Type),
		Desc:     ps.Description,
		Required: required,
		Enum:     ps.Enum,
	}
	if ps.Items != nil {
		info.ElemInfo = convertParam(ps.Items, false)
	}
	if len(ps.Properties) > 0 {
		reqSet := make(map[string]bool, len(ps.Required))
		for _, r := range ps.Required {
			reqSet[r] = true
		}
		info.SubParams = make(map[string]*schema.ParameterInfo, len(ps.Properties))
		for name, subProp := range ps.Properties {
			info.SubParams[name] = convertParam(subProp, reqSet[name])
		}
	}
	return info
}
