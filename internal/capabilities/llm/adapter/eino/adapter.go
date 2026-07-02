// Package eino - eino 框架适配器
// 将 eino model.ToolCallingChatModel 适配为我们自定义的 llm.ChatModel 接口
// 所有 domain.Message ↔ schema.Message 的转换都在此完成，外部调用者不感知 eino
package eino

import (
	"context"
	"fmt"

	einoModel "github.com/cloudwego/eino/components/model"

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
