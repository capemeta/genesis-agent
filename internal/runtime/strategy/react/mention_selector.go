package react

import (
	"context"

	skillcontract "genesis-agent/internal/capabilities/skill/contract"
)

// MentionSelector 把 SkillService.SelectForTurn 适配为 ReactLoop 的 mention 选择器。
type MentionSelector struct {
	Service        skillcontract.Service
	CatalogRequest skillcontract.CatalogRequest
}

// SelectMentions 解析用户文本中的 $skill / skill:// 引用。
func (s *MentionSelector) SelectMentions(ctx context.Context, text string) ([]SkillMention, error) {
	if s == nil || s.Service == nil {
		return nil, nil
	}
	selected, err := s.Service.SelectForTurn(ctx, skillcontract.SelectionRequest{
		CatalogRequest: s.CatalogRequest,
		Text:           text,
	})
	if err != nil {
		return nil, err
	}
	out := make([]SkillMention, 0, len(selected))
	for _, meta := range selected {
		out = append(out, SkillMention{
			Skill:    meta.QualifiedName,
			Resource: string(meta.MainResource),
		})
	}
	return out, nil
}
