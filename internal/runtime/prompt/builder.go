// Package prompt 负责构建发送给 LLM 的系统提示词
// 支持静态提示词和动态注入（记忆、技能等）
package prompt

import (
	"fmt"
	"strings"
	"time"

	"genesis-agent/internal/domain"
)

// DefaultBuilder 系统提示词构建器
type DefaultBuilder struct{}

// New 创建提示词构建器
func New() Builder {
	return &DefaultBuilder{}
}

// BuildSystem 构建System消息的提示词内容
// 注入当前时间、Agent配置的基础提示词，以及行为约束
func (b *DefaultBuilder) BuildSystem(agent *domain.Agent) string {
	var sb strings.Builder

	// 当前时间上下文
	sb.WriteString(fmt.Sprintf("当前时间: %s\n\n", time.Now().Format("2006年01月02日 15:04:05")))

	// Agent自定义系统提示词
	if agent.SystemPrompt != "" {
		sb.WriteString(agent.SystemPrompt)
		sb.WriteString("\n\n")
	} else {
		// 默认系统提示词
		sb.WriteString("你是一个有帮助的AI助手。请根据用户的问题，合理使用提供的工具来回答。\n\n")
	}

	// 行为约束
	sb.WriteString("## 行为规则\n")
	sb.WriteString("- 思考时请清晰说明你的推理过程\n")
	sb.WriteString("- 使用工具前先判断是否必要\n")
	sb.WriteString("- 工具结果需要结合上下文给出完整回答\n")
	sb.WriteString("- 直接回答用户的问题，不要重复工具的原始输出\n")

	return sb.String()
}
