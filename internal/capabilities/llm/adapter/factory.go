// Package llmadapter 负责根据应用配置选择并创建 LLM 适配器。
package llmadapter

import (
	"context"
	"fmt"

	"genesis-agent/internal/capabilities/llm/adapter/eino"
	"genesis-agent/internal/capabilities/llm/contract"
	"genesis-agent/internal/platform/config"
)

// NewChatModel 根据 chat 路由创建默认 ChatModel。
func NewChatModel(ctx context.Context, cfg config.LLMConfig) (llm.ChatModel, error) {
	resolved, err := cfg.ResolveRoute("chat")
	if err != nil {
		return nil, fmt.Errorf("llm adapter: 解析 chat 模型失败: %w", err)
	}
	return NewChatModelByConfig(ctx, resolved)
}

// NewChatModelByConfig 根据解析后的模型配置创建 ChatModel。
func NewChatModelByConfig(ctx context.Context, cfg *config.ResolvedLLMConfig) (llm.ChatModel, error) {
	switch cfg.ProviderKind {
	case "openai":
		return eino.New(ctx, &eino.Config{
			Provider:    eino.ProviderOpenAI,
			Model:       cfg.Model,
			APIKey:      cfg.APIKey,
			BaseURL:     cfg.BaseURL,
			Timeout:     cfg.Timeout,
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
			TopP:        cfg.TopP,
			ByAzure:     cfg.ByAzure,
			APIVersion:  cfg.APIVersion,
		})
	case "ark":
		return eino.New(ctx, &eino.Config{
			Provider:     eino.ProviderArk,
			Model:        cfg.Model,
			ArkAPIKey:    cfg.APIKey,
			ArkAccessKey: cfg.AccessKey,
			ArkSecretKey: cfg.SecretKey,
			BaseURL:      cfg.BaseURL,
			Timeout:      cfg.Timeout,
			MaxTokens:    cfg.MaxTokens,
			Temperature:  cfg.Temperature,
			TopP:         cfg.TopP,
		})
	case "ollama":
		return eino.New(ctx, &eino.Config{
			Provider:    eino.ProviderOllama,
			Model:       cfg.Model,
			BaseURL:     cfg.BaseURL,
			Timeout:     cfg.Timeout,
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
			TopP:        cfg.TopP,
		})
	case "anthropic":
		return nil, fmt.Errorf("llm adapter: provider=%q 已支持配置，但当前尚未接入 Anthropic/Claude 模型适配器", cfg.ProviderName)
	default:
		return nil, fmt.Errorf("llm adapter: 不支持的 provider kind=%q", cfg.ProviderKind)
	}
}
