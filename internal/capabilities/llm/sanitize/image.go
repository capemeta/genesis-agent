// Package sanitize 提供发往 LLM 前的内容门控（Per-Request）。
package sanitize

import (
	"fmt"

	"genesis-agent/internal/domain"
)

// StripImages 当 targetSupportsImage=false 时，将所有 Image part 替换为占位文本。
func StripImages(messages []*domain.Message, targetSupportsImage bool, targetAlias string) []*domain.Message {
	if targetSupportsImage || len(messages) == 0 {
		return messages
	}
	alias := targetAlias
	if alias == "" {
		alias = "unknown"
	}
	placeholder := fmt.Sprintf("[image omitted: target model %q does not support image input]", alias)
	out := make([]*domain.Message, len(messages))
	for i, m := range messages {
		if m == nil || !m.HasImageParts() {
			out[i] = m
			continue
		}
		clone := *m
		parts := make([]domain.ContentPart, 0, len(m.Parts))
		for _, p := range m.Parts {
			if p.Type == domain.ContentPartImage {
				parts = append(parts, domain.ContentPart{Type: domain.ContentPartText, Text: placeholder})
				continue
			}
			parts = append(parts, p)
		}
		clone.Parts = parts
		out[i] = &clone
	}
	return out
}

// CompactHistoricalImages 将超出 recentTurns 的含图消息中的 Image part 降级为文本摘要。
// recentTurns 按从末尾倒数的「含 Image 的消息条数」计；默认调用方传 2。
func CompactHistoricalImages(messages []*domain.Message, recentImageMessages int) []*domain.Message {
	if recentImageMessages < 0 {
		recentImageMessages = 0
	}
	if len(messages) == 0 {
		return messages
	}
	// 从尾部标记保留的含图消息下标
	keep := map[int]struct{}{}
	left := recentImageMessages
	for i := len(messages) - 1; i >= 0 && left > 0; i-- {
		if messages[i] != nil && messages[i].HasImageParts() {
			keep[i] = struct{}{}
			left--
		}
	}
	out := make([]*domain.Message, len(messages))
	for i, m := range messages {
		if m == nil || !m.HasImageParts() {
			out[i] = m
			continue
		}
		if _, ok := keep[i]; ok {
			out[i] = m
			continue
		}
		clone := *m
		parts := make([]domain.ContentPart, 0, len(m.Parts))
		for _, p := range m.Parts {
			if p.Type == domain.ContentPartImage && p.ImageRef != nil {
				name := p.ImageRef.PathAlias
				if name == "" {
					name = p.ImageRef.CandidateID
				}
				if name == "" {
					name = p.ImageRef.AttachmentID
				}
				parts = append(parts, domain.ContentPart{
					Type: domain.ContentPartText,
					Text: fmt.Sprintf("[historical image ref: %s, omitted]", name),
				})
				continue
			}
			parts = append(parts, p)
		}
		clone.Parts = parts
		out[i] = &clone
	}
	return out
}
