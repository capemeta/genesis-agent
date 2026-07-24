// Package eino - eino 框架适配器
// 将 eino model.ToolCallingChatModel 适配为我们自定义 the llm.ChatModel 接口
// 所有 domain.Message ↔ schema.Message 的转换都在此完成，外部调用者不感知 eino
package eino

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"genesis-agent/internal/capabilities/llm/contract"
	"genesis-agent/internal/capabilities/llm/sanitize"
	"genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
)

// adapter 将 eino ToolCallingChatModel 适配为 llm.ChatModel 接口
type adapter struct {
	inner         einoModel.ToolCallingChatModel // 被包裹的 eino 模型实例
	modelName     string                         // 模型名称（日志/追踪用）
	supportsImage bool                           // Per-Request Sanitizer 真相
}

// newAdapter 创建 eino 适配器，返回 llm.ChatModel 接口
func newAdapter(m einoModel.ToolCallingChatModel, modelName string, supportsImage bool) llm.ChatModel {
	return &adapter{
		inner:         m,
		modelName:     modelName,
		supportsImage: supportsImage,
	}
}

// GetModelName 实现 llm.ChatModel 接口
func (a *adapter) GetModelName() string {
	return a.modelName
}

// bindCaller 在有工具时 WithTools；无工具时直接使用 inner，避免 "no tools to bind"。
func (a *adapter) bindCaller(tools []*tool.Info) (einoModel.ToolCallingChatModel, error) {
	schemaTools := toolInfosToSchema(tools)
	if len(schemaTools) == 0 {
		return a.inner, nil
	}
	bound, err := a.inner.WithTools(schemaTools)
	if err != nil {
		return nil, fmt.Errorf("llm/eino: 工具绑定失败: %w", err)
	}
	return bound, nil
}

// Generate 实现 llm.ChatModel 接口
// 执行流程：消息格式转换 → 工具绑定 → 调用 eino → 结果格式转换
func (a *adapter) Generate(ctx context.Context, messages []*domain.Message, tools []*tool.Info) (*domain.Message, error) {
	// Step 0：Per-Request ImageSanitizer（目标模型不支持则硬剥离）
	messages = sanitize.StripImages(messages, a.supportsImage, a.modelName)
	// Step 1：domain.Message → eino schema.Message
	schemaMessages := batchDomainToSchema(messages)

	// Step 2：有工具才 WithTools；VisionExpert / 纯对话传 nil/空切片时必须跳过，
	// 否则 eino 返回 "no tools to bind"，形态 B 会瞬间失败。
	caller, err := a.bindCaller(tools)
	if err != nil {
		return nil, err
	}

	// Step 3：调用 eino 生成回复
	resp, err := caller.Generate(ctx, schemaMessages)
	if err != nil {
		return nil, fmt.Errorf("llm/eino: Generate 调用失败: %w", err)
	}
	if reason := finishReason(resp); isIncompleteFinishReason(reason) {
		return nil, fmt.Errorf("llm/eino: 模型响应不完整: finish_reason=%s", reason)
	}

	return schemaToDomain(resp), nil
}

// StreamGenerate 实现 llm.ChatModel 接口
// 执行流程：消息格式转换 → 工具绑定 → 调用 eino.Stream → 循环 Recv 分片并回调 → 聚合最终消息
func (a *adapter) StreamGenerate(ctx context.Context, messages []*domain.Message, tools []*tool.Info, onDelta func(delta string, isThought bool)) (*domain.Message, error) {
	messages = sanitize.StripImages(messages, a.supportsImage, a.modelName)
	// Step 1：domain.Message → eino schema.Message
	schemaMessages := batchDomainToSchema(messages)

	// Step 2：绑定工具（空工具跳过）
	caller, err := a.bindCaller(tools)
	if err != nil {
		return nil, err
	}

	// Step 3：调用 eino Stream 接口
	reader, err := caller.Stream(ctx, schemaMessages)
	if err != nil {
		return nil, fmt.Errorf("llm/eino: Stream 调用失败: %w", err)
	}
	defer reader.Close()

	// Step 4：循环读取流数据并聚合成 finalMsg
	var finalMsg *schema.Message
	for {
		chunk, err := reader.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("llm/eino: Stream 读取分片失败: %w", err)
		}

		if finalMsg == nil {
			role := chunk.Role
			if role == "" {
				role = schema.Assistant
			}
			finalMsg = &schema.Message{
				Role: role,
			}
		}
		if chunk.ResponseMeta != nil {
			if finalMsg.ResponseMeta == nil {
				finalMsg.ResponseMeta = &schema.ResponseMeta{}
			}
			if chunk.ResponseMeta.FinishReason != "" {
				finalMsg.ResponseMeta.FinishReason = chunk.ResponseMeta.FinishReason
			}
			if chunk.ResponseMeta.Usage != nil {
				finalMsg.ResponseMeta.Usage = chunk.ResponseMeta.Usage
			}
		}

		// 增量文字内容处理
		if chunk.Content != "" {
			finalMsg.Content += chunk.Content
			onDelta(chunk.Content, false)
		}

		// 增量推理思考内容处理
		if chunk.ReasoningContent != "" {
			finalMsg.ReasoningContent += chunk.ReasoningContent
			onDelta(chunk.ReasoningContent, true)
		}

		// 增量工具调用处理
		for _, tc := range chunk.ToolCalls {
			var index int
			if tc.Index != nil {
				index = *tc.Index
			} else {
				index = len(finalMsg.ToolCalls)
			}

			// 确保 slice 容量足够
			for len(finalMsg.ToolCalls) <= index {
				finalMsg.ToolCalls = append(finalMsg.ToolCalls, schema.ToolCall{})
			}

			existing := &finalMsg.ToolCalls[index]
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Type != "" {
				existing.Type = tc.Type
			}
			if tc.Function.Name != "" {
				existing.Function.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				existing.Function.Arguments += tc.Function.Arguments
			}
		}
	}

	if finalMsg == nil {
		return nil, fmt.Errorf("llm/eino: Stream 返回空流")
	}
	if reason := finishReason(finalMsg); isIncompleteFinishReason(reason) {
		return nil, fmt.Errorf("llm/eino: 模型流式响应不完整: finish_reason=%s", reason)
	}

	return schemaToDomain(finalMsg), nil
}

func finishReason(message *schema.Message) string {
	if message == nil || message.ResponseMeta == nil {
		return ""
	}
	return strings.TrimSpace(message.ResponseMeta.FinishReason)
}

func isIncompleteFinishReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length", "max_tokens", "max_output_tokens", "max_tokens_exceeded":
		return true
	default:
		return false
	}
}
