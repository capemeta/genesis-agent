// Package eino - domain.Message ↔ eino schema.Message 转换
// 此文件是 llm/eino 包的内部实现，不对外暴露 eino 类型
package eino

import (
	"genesis-agent/internal/domain"

	"github.com/cloudwego/eino/schema"
)

// domainToSchema 将 domain.Message 转换为 eino schema.Message
func domainToSchema(m *domain.Message) *schema.Message {
	msg := &schema.Message{
		Role:       schema.RoleType(m.Role),
		Content:    m.Content,
		ToolCallID: m.ToolCallID,
	}
	for _, tc := range m.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: schema.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return msg
}

// schemaToDomain 将 eino schema.Message 转换为 domain.Message
func schemaToDomain(m *schema.Message) *domain.Message {
	msg := &domain.Message{
		Role:       domain.RoleType(m.Role),
		Content:    m.Content,
		ToolCallID: m.ToolCallID,
	}
	for _, tc := range m.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, domain.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: domain.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return msg
}

// batchDomainToSchema 批量转换消息列表
func batchDomainToSchema(messages []*domain.Message) []*schema.Message {
	result := make([]*schema.Message, 0, len(messages))
	for _, m := range messages {
		result = append(result, domainToSchema(m))
	}
	return result
}
