// Package eino - eino 框架适配器
// 将 eino model.ToolCallingChatModel 适配为我们自定义 the llm.ChatModel 接口
// 所有 domain.Message ↔ schema.Message 的转换都在此完成，外部调用者不感知 eino
package eino

import (
	"context"
	"errors"
	"fmt"
	"io"

	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"genesis-agent/internal/capabilities/llm/contract"
	"genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
)

// adapter 将 eino ToolCallingChatModel 适配为 llm.ChatModel 接口
type adapter struct {
	inner     einoModel.ToolCallingChatModel // 被包裹的 eino 模型实例
	modelName string                         // 模型名称（日志/追踪用）
}

// newAdapter 创建 eino 适配器，返回 llm.ChatModel 接口
func newAdapter(m einoModel.ToolCallingChatModel, modelName string) llm.ChatModel {
	return &adapter{
		inner:     m,
		modelName: modelName,
	}
}

// GetModelName 实现 llm.ChatModel 接口
func (a *adapter) GetModelName() string {
	return a.modelName
}

// Generate 实现 llm.ChatModel 接口
// 执行流程：消息格式转换 → 工具绑定 → 调用 eino → 结果格式转换
func (a *adapter) Generate(ctx context.Context, messages []*domain.Message, tools []*tool.Info) (*domain.Message, error) {
	// Step 1：domain.Message → eino schema.Message
	schemaMessages := batchDomainToSchema(messages)

	// Step 2：通过 WithTools 绑定工具（返回新实例，线程安全）
	schemaTools := toolInfosToSchema(tools)
	boundModel, err := a.inner.WithTools(schemaTools)
	if err != nil {
		return nil, fmt.Errorf("llm/eino: 工具绑定失败: %w", err)
	}

	// Step 3：调用 eino 生成回复
	resp, err := boundModel.Generate(ctx, schemaMessages)
	if err != nil {
		return nil, fmt.Errorf("llm/eino: Generate 调用失败: %w", err)
	}

	// Step 4：eino schema.Message → domain.Message
	return schemaToDomain(resp), nil
}

// StreamGenerate 实现 llm.ChatModel 接口
// 执行流程：消息格式转换 → 工具绑定 → 调用 eino.Stream → 循环 Recv 分片并回调 → 聚合最终消息
func (a *adapter) StreamGenerate(ctx context.Context, messages []*domain.Message, tools []*tool.Info, onDelta func(delta string, isThought bool)) (*domain.Message, error) {
	// Step 1：domain.Message → eino schema.Message
	schemaMessages := batchDomainToSchema(messages)

	// Step 2：绑定工具
	schemaTools := toolInfosToSchema(tools)
	boundModel, err := a.inner.WithTools(schemaTools)
	if err != nil {
		return nil, fmt.Errorf("llm/eino: 流式工具绑定失败: %w", err)
	}

	// Step 3：调用 eino Stream 接口
	reader, err := boundModel.Stream(ctx, schemaMessages)
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

	return schemaToDomain(finalMsg), nil
}
