package context

import (
	"context"
	"fmt"
	"strings"

	"genesis-agent/internal/domain"
)

// TruncateStrategy 单条消息超限时的就地截断策略枚举。
type TruncateStrategy string

const (
	// TruncateHeadTail 保留首尾各 HeadRatio 比例，丢弃中间。
	// 适用：user_turn（长输入）、conversation_summary（摘要正文）。
	TruncateHeadTail TruncateStrategy = "head_tail"

	// TruncateTailOnly 只保留尾部最多 budget 个 token，截去头部。
	// 适用：tool_result（命令输出、代码运行结果）、assistant（长回复）。
	TruncateTailOnly TruncateStrategy = "tail_only"

	// TruncateTurnBoundary 按对话轮边界删除最老的完整对（由短期记忆 GetRecent 使用）。
	TruncateTurnBoundary TruncateStrategy = "turn_boundary"

	// TruncateSectionAware 保留完整 Markdown 段落，从优先级最低的段落删起。
	// 适用：skill_injection（SKILL.md 注入）。
	TruncateSectionAware TruncateStrategy = "section_aware"
)

// TruncateConfig 截断策略的配置。
type TruncateConfig struct {
	Strategy        TruncateStrategy
	HeadRatio       float64        // 仅 TruncateHeadTail 有效，默认 0.5
	SectionPriority map[string]int // key=段落标题关键词, value=优先级（越小越优先删）
}

// TokenEstimatorForTruncate 截断逻辑专用的最小估算器接口
type TokenEstimatorForTruncate interface {
	Estimate(ctx context.Context, text string, model string) int
}

// DefaultConfigForKind 根据消息的语义类型返回方案约定的默认就地截断配置
func DefaultConfigForKind(kind domain.MessageKind) TruncateConfig {
	switch kind {
	case domain.MessageKindToolResult:
		return TruncateConfig{Strategy: TruncateTailOnly}
	case domain.MessageKindUserTurn:
		return TruncateConfig{Strategy: TruncateHeadTail, HeadRatio: 0.5}
	case domain.MessageKindAssistant:
		return TruncateConfig{Strategy: TruncateTailOnly}
	case domain.MessageKindSkillInjection:
		return TruncateConfig{Strategy: TruncateSectionAware}
	case domain.MessageKindConversationSummary:
		return TruncateConfig{Strategy: TruncateHeadTail, HeadRatio: 0.6}
	case domain.MessageKindReminder:
		return TruncateConfig{Strategy: TruncateTailOnly}
	default:
		return TruncateConfig{Strategy: TruncateTailOnly}
	}
}

// Truncate 根据配置就地截断单条 Message。
// 如果 Message 的 Token 数低于或等于 budget，则直接返回原消息，不做任何修改。
// 若 cfg.Strategy 为空，则自动根据 msg.NormalizedKind() 匹配并加载默认截断策略。
func Truncate(ctx context.Context, msg *domain.Message, budget int, model string, estimator TokenEstimatorForTruncate, cfg TruncateConfig) *domain.Message {
	if msg == nil || estimator == nil || budget <= 0 {
		return msg
	}

	tokens := estimator.Estimate(ctx, msg.Content, model)
	if tokens <= budget {
		return msg
	}

	// 自动选用默认配置
	if cfg.Strategy == "" {
		cfg = DefaultConfigForKind(msg.NormalizedKind())
	}

	// 复制一份，防止脏写 domain 内存
	newMsg := &domain.Message{
		UUID:             msg.UUID,
		Role:             msg.Role,
		ToolCalls:        msg.ToolCalls,
		ToolCallID:       msg.ToolCallID,
		ReasoningContent: msg.ReasoningContent,
		Kind:             msg.Kind,
		Source:           msg.Source,
	}

	switch cfg.Strategy {
	case TruncateTailOnly:
		newMsg.Content = truncateTailOnly(ctx, msg.Content, budget, model, estimator)
	case TruncateHeadTail:
		ratio := cfg.HeadRatio
		if ratio <= 0 || ratio >= 1 {
			ratio = 0.5
		}
		newMsg.Content = truncateHeadTail(ctx, msg.Content, budget, ratio, model, estimator)
	case TruncateSectionAware:
		newMsg.Content = truncateSectionAware(ctx, msg.Content, budget, cfg.SectionPriority, model, estimator)
	default:
		// 默认回退到 TruncateTailOnly
		newMsg.Content = truncateTailOnly(ctx, msg.Content, budget, model, estimator)
	}

	return newMsg
}

// truncateTailOnly 保留尾部
func truncateTailOnly(ctx context.Context, text string, budget int, model string, estimator TokenEstimatorForTruncate) string {
	runes := []rune(text)
	total := len(runes)
	if total == 0 {
		return text
	}

	// 二分法找到满足 budget 限制的最大尾部切片
	low, high := 0, total
	bestIdx := total
	for low <= high {
		mid := (low + high) / 2
		tailContent := string(runes[mid:])
		tokens := estimator.Estimate(ctx, tailContent, model)
		if tokens <= budget {
			bestIdx = mid
			high = mid - 1 // 尝试往左，留更多字符
		} else {
			low = mid + 1
		}
	}

	removedRunes := bestIdx
	if removedRunes <= 0 {
		return text
	}

	return fmt.Sprintf("[头部截断 %d]\n%s", removedRunes, string(runes[bestIdx:]))
}

// truncateHeadTail 首尾保留，丢弃中间
func truncateHeadTail(ctx context.Context, text string, budget int, headRatio float64, model string, estimator TokenEstimatorForTruncate) string {
	runes := []rune(text)
	total := len(runes)
	if total == 0 {
		return text
	}

	headBudget := int(float64(budget) * headRatio)
	tailBudget := budget - headBudget
	if headBudget <= 0 {
		return truncateTailOnly(ctx, text, budget, model, estimator)
	}

	// 找头部最大容纳量
	low, high := 0, total
	bestHeadIdx := 0
	for low <= high {
		mid := (low + high) / 2
		headContent := string(runes[:mid])
		tokens := estimator.Estimate(ctx, headContent, model)
		if tokens <= headBudget {
			bestHeadIdx = mid
			low = mid + 1
		} else {
			high = mid - 1
		}
	}

	// 找尾部最大容纳量
	low, high = bestHeadIdx, total
	bestTailIdx := total
	for low <= high {
		mid := (low + high) / 2
		tailContent := string(runes[mid:])
		tokens := estimator.Estimate(ctx, tailContent, model)
		if tokens <= tailBudget {
			bestTailIdx = mid
			high = mid - 1
		} else {
			low = mid + 1
		}
	}

	if bestHeadIdx >= bestTailIdx {
		return text // 刚好装得下，不发生截断
	}

	removed := total - (bestHeadIdx + (total - bestTailIdx))
	return fmt.Sprintf("%s\n\n[中间截断 %d]\n\n%s",
		string(runes[:bestHeadIdx]), removed, string(runes[bestTailIdx:]))
}

// truncateSectionAware 基于 Markdown 章节优先级的整段删除
func truncateSectionAware(ctx context.Context, text string, budget int, priorities map[string]int, model string, estimator TokenEstimatorForTruncate) string {
	// 极简实现：按 "\n##" 划分段落
	lines := strings.Split(text, "\n")
	type section struct {
		title    string
		content  []string
		priority int
	}

	var sections []section
	currentSec := section{title: "Header", priority: 100} // 默认头优先级最高，最后才删

	for _, line := range lines {
		if strings.HasPrefix(line, "##") {
			// 先保存上一段
			sections = append(sections, currentSec)

			// 解析段标题和优先级
			title := strings.TrimSpace(line)
			prio := 50 // 默认中等
			for key, val := range priorities {
				if strings.Contains(title, key) {
					prio = val
					break
				}
			}
			currentSec = section{
				title:    title,
				content:  []string{line},
				priority: prio,
			}
		} else {
			currentSec.content = append(currentSec.content, line)
		}
	}
	sections = append(sections, currentSec)

	// 计算当前总 token
	buildText := func(secs []section) string {
		var sb strings.Builder
		for i, sec := range secs {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(strings.Join(sec.content, "\n"))
		}
		return sb.String()
	}

	currentText := buildText(sections)
	tokens := estimator.Estimate(ctx, currentText, model)
	if tokens <= budget {
		return text
	}

	// 循环删除优先级最小的段落，直到满足 budget
	for tokens > budget && len(sections) > 1 {
		// 寻找当前段落里优先级最低的（不删 Header 除非只剩 Header）
		lowestIdx := -1
		lowestPrio := 999999
		for i, sec := range sections {
			if i == 0 {
				continue // 保留 Header 优先
			}
			if sec.priority < lowestPrio {
				lowestPrio = sec.priority
				lowestIdx = i
			}
		}

		if lowestIdx == -1 {
			break // 无法进一步删段
		}

		// 替换该段的内容为截断提示占位
		sections[lowestIdx].content = []string{fmt.Sprintf("\n[段落截断: %s]\n", sections[lowestIdx].title)}
		// 提升优先级，防二次选中
		sections[lowestIdx].priority = 999999

		currentText = buildText(sections)
		tokens = estimator.Estimate(ctx, currentText, model)
	}

	return currentText
}
