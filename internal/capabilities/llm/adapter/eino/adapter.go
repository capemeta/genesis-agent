// Package eino - eino 框架适配器
// 将 eino model.ToolCallingChatModel 适配为我们自定义 the llm.ChatModel 接口
// 所有 domain.Message ↔ schema.Message 的转换都在此完成，外部调用者不感知 eino
package eino

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		logLLMCall(a.modelName, messages, tools, nil, err)
		return nil, fmt.Errorf("llm/eino: Generate 调用失败: %w", err)
	}
	if reason := finishReason(resp); isIncompleteFinishReason(reason) {
		err = fmt.Errorf("llm/eino: 模型响应不完整: finish_reason=%s", reason)
		logLLMCall(a.modelName, messages, tools, nil, err)
		return nil, err
	}

	respMsg := schemaToDomain(resp)
	logLLMCall(a.modelName, messages, tools, respMsg, nil)
	return respMsg, nil
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
		err := fmt.Errorf("llm/eino: Stream 返回空流")
		logLLMCall(a.modelName, messages, tools, nil, err)
		return nil, err
	}
	if reason := finishReason(finalMsg); isIncompleteFinishReason(reason) {
		err := fmt.Errorf("llm/eino: 模型流式响应不完整: finish_reason=%s", reason)
		logLLMCall(a.modelName, messages, tools, nil, err)
		return nil, err
	}

	respMsg := schemaToDomain(finalMsg)
	logLLMCall(a.modelName, messages, tools, respMsg, nil)
	return respMsg, nil
}

type llmCallLog struct {
	Timestamp string            `json:"timestamp"`
	Model     string            `json:"model"`
	Messages  []*domain.Message `json:"messages"`
	Tools     []*tool.Info      `json:"tools"`
	Response  *domain.Message   `json:"response"`
	Error     string            `json:"error,omitempty"`
}

func logLLMCall(modelName string, messages []*domain.Message, tools []*tool.Info, response *domain.Message, err error) {
	// 原始 LLM 消息可能含用户文档、工具参数和凭据，仅允许显式诊断时落盘。
	// 常规运行的结构化诊断走 logger/trace，并采用摘要与脱敏字段。
	if !rawLLMDebugEnabled() {
		return
	}
	logDir := filepath.Join(".", ".genesis", "logs")
	_ = os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, "llm_raw_debug.jsonl")

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	call := llmCallLog{
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Model:     modelName,
		Messages:  messages,
		Tools:     tools,
		Response:  response,
		Error:     errMsg,
	}

	data, errMarshal := json.Marshal(call)
	if errMarshal != nil {
		return
	}

	f, errOpen := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if errOpen != nil {
		return
	}
	defer f.Close()

	_, _ = f.Write(append(data, '\n'))
}

func rawLLMDebugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GENESIS_LLM_RAW_DEBUG"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
