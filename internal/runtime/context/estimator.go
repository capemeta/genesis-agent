package context

import (
	"context"
	"unicode"

	"genesis-agent/internal/domain"
)

// TokenEstimator Token 估算接口（预算决策用；实际计费以 provider usage 为权威）。
type TokenEstimator interface {
	// Estimate 估算单段文本的 token 数。
	Estimate(ctx context.Context, text string, model string) int
	// EstimateMessages 批量估算消息列表（含 role/分隔符 per-message overhead，比逐条调用 Estimate 更准）。
	EstimateMessages(ctx context.Context, msgs []*domain.Message, model string) int
}

// HeuristicEstimator 启发式 Token 估算器，对中文（CJK）和英文进行启发式加权估算。
type HeuristicEstimator struct{}

// NewHeuristicEstimator 创建启发式估算器
func NewHeuristicEstimator() TokenEstimator {
	return &HeuristicEstimator{}
}

// Estimate 估算单段文本。
func (e *HeuristicEstimator) Estimate(_ context.Context, text string, _ string) int {
	if text == "" {
		return 0
	}

	var cnChars int
	var enWords int
	inWord := false

	for _, r := range text {
		if isCJK(r) {
			cnChars++
			if inWord {
				inWord = false
			}
		} else if unicode.IsSpace(r) || unicode.IsPunct(r) {
			if inWord {
				inWord = false
			}
		} else {
			if !inWord {
				enWords++
				inWord = true
			}
		}
	}

	// 经典公式：中文汉字按 ~1.5 tokens 算，英文单词按 ~1.3 tokens 算
	return int(float64(cnChars)*1.5 + float64(enWords)*1.3)
}

// EstimateMessages 批量估算消息列表。
func (e *HeuristicEstimator) EstimateMessages(ctx context.Context, msgs []*domain.Message, model string) int {
	if len(msgs) == 0 {
		return 0
	}

	total := 0
	for _, m := range msgs {
		if m == nil {
			continue
		}
		// 每一条消息的系统级 Overhead（Role 与包装符），默认加 4 tokens 
		total += 4
		total += e.Estimate(ctx, m.Content, model)
		
		// 估算 ToolCalls
		for _, tc := range m.ToolCalls {
			total += 6 // tool call wrapper overhead
			total += e.Estimate(ctx, tc.Function.Name, model)
			total += e.Estimate(ctx, tc.Function.Arguments, model)
		}
		if m.ToolCallID != "" {
			total += e.Estimate(ctx, m.ToolCallID, model)
		}
		if m.ReasoningContent != "" {
			total += e.Estimate(ctx, m.ReasoningContent, model)
		}
	}

	// 整个对话最后的 assistant 包装符，通常加 3 tokens
	total += 3
	return total
}

func isCJK(r rune) bool {
	// 常用中日韩汉字字符集区间
	return unicode.Is(unicode.Han, r) ||
		(r >= 0x3000 && r <= 0x303F) || // CJK 标点符号
		(r >= 0x3040 && r <= 0x309F) || // 日文平假名
		(r >= 0x30A0 && r <= 0x30FF) || // 日文片假名
		(r >= 0xFF00 && r <= 0xFFEF)    // 半角/全角字符
}
