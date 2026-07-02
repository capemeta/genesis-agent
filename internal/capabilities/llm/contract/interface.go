// Package llm 定义 LLM 对话模型的抽象接口
// 接口使用我们自己的 domain.Message 类型，与具体框架（eino/openai-sdk等）完全解耦
// eino 只是此接口的一个实现，未来可以不经过 eino 直接对接任何 LLM
package llm

import (
	"context"

	"genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
)

// ChatModel LLM 对话模型接口
// 实现者负责处理：消息格式转换、工具绑定、重试、超时等细节
// 调用者（engine）只关心：发消息、收回复、有没有工具调用
type ChatModel interface {
	// Generate 同步生成回复
	// messages: 完整对话历史（system + 历史 + 工具结果）
	// tools:    本次 Run 可用的工具元信息（空切片=不启用工具调用）
	Generate(ctx context.Context, messages []*domain.Message, tools []*tool.Info) (*domain.Message, error)

	// GetModelName 返回当前模型名称（用于日志/追踪）
	GetModelName() string
}
