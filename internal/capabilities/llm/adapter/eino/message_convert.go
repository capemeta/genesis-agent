// Package eino - domain.Message ↔ eino schema.Message 转换
// 此文件是 llm/eino 包的内部实现，不对外暴露 eino 类型
package eino

import (
	"genesis-agent/internal/capabilities/media/materialize"
	"genesis-agent/internal/domain"

	"github.com/cloudwego/eino/schema"
)

// domainToSchema 将 domain.Message 转换为 eino schema.Message
func domainToSchema(m *domain.Message) *schema.Message {
	msg := &schema.Message{
		Role:             schema.RoleType(m.Role),
		Content:          m.TextContent(),
		ToolCallID:       m.ToolCallID,
		ReasoningContent: m.ReasoningContent,
	}
	if parts := toUserInputMultiContent(m); len(parts) > 0 {
		msg.UserInputMultiContent = parts
		// 多模态输入时以 UserInputMultiContent 为准
		msg.Content = ""
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

func toUserInputMultiContent(m *domain.Message) []schema.MessageInputPart {
	if m == nil || len(m.Parts) == 0 || !m.HasImageParts() {
		return nil
	}
	out := make([]schema.MessageInputPart, 0, len(m.Parts))
	for _, p := range m.Parts {
		switch p.Type {
		case domain.ContentPartText:
			if p.Text == "" {
				continue
			}
			out = append(out, schema.MessageInputPart{Type: schema.ChatMessagePartTypeText, Text: p.Text})
		case domain.ContentPartImage:
			if p.ImageRef == nil {
				continue
			}
			url, err := materialize.ToDataURL(p.ImageRef)
			if err != nil {
				out = append(out, schema.MessageInputPart{
					Type: schema.ChatMessagePartTypeText,
					Text: materialize.PlaceholderFor(err, p.ImageRef),
				})
				continue
			}
			detail := schema.ImageURLDetailAuto
			switch p.ImageRef.Detail {
			case "low":
				detail = schema.ImageURLDetailLow
			case "high":
				detail = schema.ImageURLDetailHigh
			}
			out = append(out, schema.MessageInputPart{
				Type: schema.ChatMessagePartTypeImageURL,
				Image: &schema.MessageInputImage{
					MessagePartCommon: schema.MessagePartCommon{
						URL:      &url,
						MIMEType: p.ImageRef.MediaType,
					},
					Detail: detail,
				},
			})
		}
	}
	return out
}

// schemaToDomain 将 eino schema.Message 转换为 domain.Message
func schemaToDomain(m *schema.Message) *domain.Message {
	msg := &domain.Message{
		Role:             domain.RoleType(m.Role),
		Content:          m.Content,
		ToolCallID:       m.ToolCallID,
		ReasoningContent: m.ReasoningContent,
	}
	for _, tc := range m.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, domain.ToolCall{
			ID:       tc.ID,
			Type:     tc.Type,
			Function: domain.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
		})
	}
	msg.EnsureKind() // LLM 回写默认 assistant/tool；不覆盖调用方已设 Kind
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
