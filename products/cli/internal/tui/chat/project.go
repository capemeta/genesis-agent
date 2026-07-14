package chat

import (
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/transcript"
)

// projectUIMessages 将短期记忆完整链投影为 CLI 聊天气泡。
// 内核统一用 Kind；CLI 只选 UIPolicy，不另写存储。
func projectUIMessages(msgs []*domain.Message) []uiMessage {
	projected := transcript.ProjectForUI(msgs, transcript.DefaultCLIPolicy())
	if len(projected) == 0 {
		return nil
	}
	out := make([]uiMessage, 0, len(projected))
	for _, m := range projected {
		if m == nil {
			continue
		}
		out = append(out, domainToUIMessage(m))
	}
	return out
}

func domainToUIMessage(m *domain.Message) uiMessage {
	role := "system"
	switch m.NormalizedKind() {
	case domain.MessageKindUserTurn:
		role = "user"
	case domain.MessageKindAssistant:
		role = "assistant"
	case domain.MessageKindConversationSummary:
		role = "system"
	default:
		switch m.Role {
		case domain.RoleUser:
			role = "user"
		case domain.RoleAssistant:
			role = "assistant"
		}
	}
	return uiMessage{role: role, content: m.Content}
}
