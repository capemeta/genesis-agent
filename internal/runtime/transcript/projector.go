// Package transcript 提供会话消息的双投影（ForModel / ForUI）。
//
// 统一：投影规则以 domain.Message.Kind 为唯一主开关，三端同源算法。
// 独立：产品通过 UIPolicy 微调展示（CLI 先落地；Desktop/Enterprise 只换策略，不改存储）。
//
// 契约见 docs/会话管理与记忆管理设计方案.md §6.2.3。
package transcript

import "genesis-agent/internal/domain"

// Projector 消息投影端口（产品可替换实现，默认用 Default）。
type Projector interface {
	ForModel(msgs []*domain.Message) []*domain.Message
	ForUI(msgs []*domain.Message, policy UIPolicy) []*domain.Message
}

// UIPolicy 产品侧 UI 投影策略（不进持久化，不改 Kind）。
type UIPolicy struct {
	// ShowConversationSummary 是否展示压缩摘要气泡（默认 true）
	ShowConversationSummary bool
	// ShowSystemDiagnostics 是否展示宿主诊断 system（如 repeat_guard）；默认 false
	ShowSystemDiagnostics bool
}

// DefaultCLIPolicy CLI 默认策略：聊天气泡只看真人对话 + 助手可见正文 + 可选摘要。
func DefaultCLIPolicy() UIPolicy {
	return UIPolicy{
		ShowConversationSummary: true,
		ShowSystemDiagnostics:   false,
	}
}

// DefaultEnterprisePolicy 企业默认策略入口（当前与 CLI 同默认值；产品可覆盖 UIPolicy，勿在此硬编码租户差异）。
func DefaultEnterprisePolicy() UIPolicy {
	return UIPolicy{
		ShowConversationSummary: true,
		ShowSystemDiagnostics:   false,
	}
}

// Default 默认投影器（三端可共用）。
type Default struct{}

// ForModel 送给 LLM 的上下文：完整链 + EnsureKind。
func (Default) ForModel(msgs []*domain.Message) []*domain.Message {
	return domain.ForModel(msgs)
}

// ForUI 聊天气泡投影：以 domain.IsChatVisibleMessage 为基线，再用 UIPolicy 微调。
func (Default) ForUI(msgs []*domain.Message, policy UIPolicy) []*domain.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]*domain.Message, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if !visibleInChat(m, policy) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// visibleInChat 在 domain 默认可见性之上叠加产品策略，避免与 IsChatVisibleMessage 双轨漂移。
func visibleInChat(m *domain.Message, policy UIPolicy) bool {
	kind := m.NormalizedKind()
	switch kind {
	case domain.MessageKindConversationSummary:
		return policy.ShowConversationSummary
	case domain.MessageKindSystem:
		return policy.ShowSystemDiagnostics
	default:
		return domain.IsChatVisibleMessage(m)
	}
}

// ProjectForModel 包级便捷方法。
func ProjectForModel(msgs []*domain.Message) []*domain.Message {
	return Default{}.ForModel(msgs)
}

// ProjectForUI 包级便捷方法。
func ProjectForUI(msgs []*domain.Message, policy UIPolicy) []*domain.Message {
	return Default{}.ForUI(msgs, policy)
}
