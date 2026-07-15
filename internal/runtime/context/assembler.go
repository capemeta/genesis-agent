package context

import (
	"context"
	"fmt"
	"strings"

	"genesis-agent/internal/domain"
)

// AssemblerOptions 装配最终 LLM 上下文消息的输入参数
type AssemblerOptions struct {
	Budget         DistributableBudget // 由 Planner 计算出的额度约束
	Model          string              // 目标模型
	MaxInputTokens int                 // 最终输入硬上限；0 表示由调用方关闭

	SystemPrompt     string                  // 基础 Persona + 稳定 Instructions
	UserProfile      *domain.UserProfile     // 用户画像数据 (LTM级，选注到 system)
	LongTermMemories []*domain.LongTermEntry // 长期记忆召回条目 (LTM级，按复合排序降序，选注到 system)

	HistorySummary  *domain.SessionSummary // 历史摘要 (Kind=conversation_summary，插到 history 头部)
	HistoryMessages []*domain.Message      // 已持久化的跨 Run 历史对话 (GetRecent)

	WorkingMessages    []*domain.Message // RunContext 中本 Run 当前产生的工具执行轨迹
	CurrentUserMessage *domain.Message   // 本轮当前用户输入 (Kind=user_turn)
	ReminderMessage    *domain.Message   // 本轮附加的 reminder (Kind=reminder)
}

// ContextAssembler 负责上下文裁剪与最终 messages 拼装的内核接口
type ContextAssembler interface {
	Assemble(ctx context.Context, opt AssemblerOptions) ([]*domain.Message, error)
}

// DefaultContextAssembler 默认的上下文装配器实现
type DefaultContextAssembler struct {
	estimator TokenEstimator
}

// NewDefaultContextAssembler 创建默认上下文装配器
func NewDefaultContextAssembler(estimator TokenEstimator) ContextAssembler {
	return &DefaultContextAssembler{estimator: estimator}
}

// Assemble 根据各弹性段预算限制进行裁剪拼装
func (a *DefaultContextAssembler) Assemble(ctx context.Context, opt AssemblerOptions) ([]*domain.Message, error) {
	var messages []*domain.Message

	// 1. 装配 system 消息
	systemContent := a.assembleSystemPrompt(ctx, opt)
	messages = append(messages, domain.NewSystemMessage(systemContent))

	// 2. 装配 history (含摘要 + 裁剪后的历史消息)
	historyPart := a.assembleHistory(ctx, opt)
	messages = append(messages, historyPart...)

	// 3. 装配 working_obs (当前 Run 中间工具轨迹，不与 history 重叠)
	for _, m := range opt.WorkingMessages {
		if m != nil {
			messages = append(messages, m)
		}
	}

	// 4. 装配 user (当前输入 + reminder)
	if opt.CurrentUserMessage != nil {
		messages = append(messages, opt.CurrentUserMessage)
	}
	if opt.ReminderMessage != nil {
		messages = append(messages, opt.ReminderMessage)
	}

	return a.enforceHardLimit(ctx, messages, opt)
}

func (a *DefaultContextAssembler) enforceHardLimit(ctx context.Context, messages []*domain.Message, opt AssemblerOptions) ([]*domain.Message, error) {
	if opt.MaxInputTokens <= 0 || a.estimator.EstimateMessages(ctx, messages, opt.Model) <= opt.MaxInputTokens {
		return messages, nil
	}
	result := append([]*domain.Message(nil), messages...)
	// 优先压缩可从持久化工件恢复或可由模型重建的内容，最后才压缩真人输入。
	priorities := []domain.MessageKind{domain.MessageKindToolResult, domain.MessageKindSkillInjection, domain.MessageKindAssistant, domain.MessageKindConversationSummary, domain.MessageKindReminder, domain.MessageKindUserTurn}
	for _, kind := range priorities {
		for i := range result {
			if result[i] == nil || result[i].NormalizedKind() != kind {
				continue
			}
			total := a.estimator.EstimateMessages(ctx, result, opt.Model)
			if total <= opt.MaxInputTokens {
				return result, nil
			}
			current := a.estimator.EstimateMessages(ctx, []*domain.Message{result[i]}, opt.Model)
			budget := current - (total - opt.MaxInputTokens)
			if budget < 32 {
				budget = 32
			}
			result[i] = Truncate(ctx, result[i], budget, opt.Model, a.estimator, TruncateConfig{})
		}
	}
	if actual := a.estimator.EstimateMessages(ctx, result, opt.Model); actual > opt.MaxInputTokens {
		return nil, fmt.Errorf("assembled context exceeds input limit: actual=%d limit=%d", actual, opt.MaxInputTokens)
	}
	return result, nil
}

// 组装 System Prompt (注入 LTM 列表和画像)
func (a *DefaultContextAssembler) assembleSystemPrompt(ctx context.Context, opt AssemblerOptions) string {
	var sb strings.Builder
	sb.WriteString(opt.SystemPrompt)

	// 注入长期记忆段 (LTM)，受 LTM 预算额度约束
	if len(opt.LongTermMemories) > 0 && opt.Budget.LTM > 0 {
		var ltmLines []string
		usedTokens := 0

		// 已经排好序的记忆（ compositeScore 降序）
		for _, memory := range opt.LongTermMemories {
			if memory == nil || strings.TrimSpace(memory.Content) == "" {
				continue
			}
			line := fmt.Sprintf("- %s", memory.Content)
			lineTokens := a.estimator.Estimate(ctx, line, opt.Model)
			if usedTokens+lineTokens > opt.Budget.LTM {
				break // 超额截断
			}
			ltmLines = append(ltmLines, line)
			usedTokens += lineTokens
		}

		if len(ltmLines) > 0 {
			sb.WriteString("\n\n<long_term_memories>\n")
			sb.WriteString(strings.Join(ltmLines, "\n"))
			sb.WriteString("\n</long_term_memories>")
		}
	}

	// 注入用户画像段 (UserProfile)
	if opt.UserProfile != nil {
		profileBlock := formatUserProfile(opt.UserProfile)
		if profileBlock != "" {
			sb.WriteString("\n\n<user_profile>\n")
			sb.WriteString(profileBlock)
			sb.WriteString("\n</user_profile>")
		}
	}

	return sb.String()
}

// 组装并裁剪历史记录
func (a *DefaultContextAssembler) assembleHistory(ctx context.Context, opt AssemblerOptions) []*domain.Message {
	var historyMsgs []*domain.Message

	// 放入滚动摘要 (如果有，且在预算之内)
	if opt.HistorySummary != nil && opt.Budget.Summary > 0 {
		summaryMsg := domain.NewConversationSummaryMessage(opt.HistorySummary.Content)
		summaryTokens := a.estimator.EstimateMessages(ctx, []*domain.Message{summaryMsg}, opt.Model)
		if summaryTokens <= opt.Budget.Summary {
			historyMsgs = append(historyMsgs, summaryMsg)
		}
	}

	// 逆序回溯累加已持久化历史对话以满足 history 预算约束
	if len(opt.HistoryMessages) > 0 && opt.Budget.History > 0 {
		var selected []*domain.Message
		usedTokens := 0

		// 从最新消息往回倒推累加
		for i := len(opt.HistoryMessages) - 1; i >= 0; i-- {
			msg := opt.HistoryMessages[i]
			if msg == nil {
				continue
			}

			msgTokens := a.estimator.EstimateMessages(ctx, []*domain.Message{msg}, opt.Model)
			if usedTokens+msgTokens > opt.Budget.History {
				break // 超预算，停止加载更老历史
			}

			// 插入到 selected 的头部以维持正确的对话时序
			selected = append([]*domain.Message{msg}, selected...)
			usedTokens += msgTokens
		}

		// 并入到滚动摘要之后
		historyMsgs = append(historyMsgs, selected...)
	}

	return historyMsgs
}

// 格式化用户画像内置字段和自定义字段为文本呈现
func formatUserProfile(profile *domain.UserProfile) string {
	var parts []string
	b := profile.Builtin

	if b.Locale != "" {
		parts = append(parts, fmt.Sprintf("Locale: %s", b.Locale))
	}
	if b.CommunicationStyle != "" {
		parts = append(parts, fmt.Sprintf("CommunicationStyle: %s", b.CommunicationStyle))
	}
	if len(b.ToolPreferences) > 0 {
		parts = append(parts, fmt.Sprintf("PreferredTools: %s", strings.Join(b.ToolPreferences, ", ")))
	}
	if b.Timezone != "" {
		parts = append(parts, fmt.Sprintf("Timezone: %s", b.Timezone))
	}
	if b.ResponseVerbosity != "" {
		parts = append(parts, fmt.Sprintf("ResponseVerbosity: %s", b.ResponseVerbosity))
	}

	for k, v := range profile.CustomFields {
		parts = append(parts, fmt.Sprintf("%s: %v", k, v))
	}

	if len(parts) == 0 {
		return ""
	}
	return "- " + strings.Join(parts, "\n- ")
}
