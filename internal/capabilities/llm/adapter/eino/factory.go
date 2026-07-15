// Package eino 是 llm.ChatModel 接口的 Eino 框架实现。
// 本包只处理 Eino SDK 相关的模型创建、消息转换和工具格式转换。
// 通用的 LLM provider 选择逻辑放在 adapters/llm 根包。
//
// 使用 cloudwego/eino-ext 的官方 Provider 组件创建模型实例。
// 支持：OpenAI / DeepSeek / Qwen / Azure / 火山引擎Ark / Ollama。
//
// 使用方式：通常由 adapters/llm 根包调用；业务入口不直接依赖本包。
package eino

import (
	"context"
	"fmt"
	"time"

	einoark "github.com/cloudwego/eino-ext/components/model/ark"
	einoollama "github.com/cloudwego/eino-ext/components/model/ollama"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	einoModel "github.com/cloudwego/eino/components/model"

	"genesis-agent/internal/capabilities/llm/contract"
)

// Provider 是当前 Eino 适配器支持的 LLM 服务商类型。
type Provider string

const (
	// ProviderOpenAI 标准 OpenAI API（也兼容所有OpenAI-Compatible服务）
	// BaseURL 留空使用官方API；填写自定义URL可对接 DeepSeek / Qwen / Moonshot 等
	ProviderOpenAI Provider = "openai"

	// ProviderArk 火山引擎 Ark（字节跳动/豆包大模型）
	// 需要 ArkAPIKey 或 ArkAccessKey + ArkSecretKey
	// Model 填写火山引擎平台的接入点 endpoint_id
	ProviderArk Provider = "ark"

	// ProviderOllama 本地 Ollama 服务（llama3, qwen2.5, gemma3 等开源模型）
	// BaseURL 默认 http://localhost:11434
	ProviderOllama Provider = "ollama"
)

// Config 是 Eino 适配器的模型配置，不同 Provider 使用不同字段组合。
// 它不是全局 LLM 配置；全局配置定义在 infra/config，由 adapters/llm 根包映射到这里。
type Config struct {
	// Provider 服务商类型（必填）
	Provider Provider

	// ---- 通用字段 ----
	// Model 模型名称（必填）
	// OpenAI:  "gpt-4o", "gpt-4o-mini"
	// Ark:     火山引擎接入点ID
	// Ollama:  "llama3", "qwen2.5:7b"
	Model string

	// ---- OpenAI / OpenAI-Compatible 字段 ----
	// APIKey API密钥（OpenAI必填，以及DeepSeek/Qwen等兼容服务）
	APIKey string

	// BaseURL 自定义API基础URL（空=OpenAI官方；填写可切换到任意兼容服务）
	// OpenAI 官方:   留空或 "https://api.openai.com/v1"
	// DeepSeek:      "https://api.deepseek.com/v1"
	// 通义千问 Qwen:  "https://dashscope.aliyuncs.com/compatible-mode/v1"
	// Moonshot/Kimi: "https://api.moonshot.cn/v1"
	BaseURL string

	// Timeout HTTP超时（0=使用默认值60秒）
	Timeout time.Duration
	// 生成参数由上层模型配置透传；nil/0 表示交由 Provider 默认值处理。
	MaxTokens   int
	Temperature *float64
	TopP        *float64

	// ---- Azure OpenAI 专用字段（ByAzure=true时使用）----
	ByAzure    bool   // 是否使用 Azure OpenAI Service
	APIVersion string // Azure API版本，如 "2024-02-01"

	// ---- 火山引擎 Ark 专用认证字段（二选一）----
	ArkAPIKey    string // Ark APIKey认证（优先级高于 AK/SK）
	ArkAccessKey string // 访问密钥ID（ArkAPIKey为空时使用）
	ArkSecretKey string // 访问密钥Secret
}

// New 根据 cfg.Provider 创建对应的 llm.ChatModel 实例
// 返回我们自定义的 llm.ChatModel 接口，底层由 eino 适配器实现
func New(ctx context.Context, cfg *Config) (llm.ChatModel, error) {
	if cfg.Provider == "" {
		return nil, fmt.Errorf("llm/eino: Config.Provider 不能为空")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("llm/eino: Config.Model 不能为空")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	var (
		einoM einoModel.ToolCallingChatModel
		err   error
	)
	switch cfg.Provider {
	case ProviderOpenAI:
		einoM, err = createOpenAI(ctx, cfg, timeout)
	case ProviderArk:
		einoM, err = createArk(ctx, cfg, timeout)
	case ProviderOllama:
		einoM, err = createOllama(ctx, cfg, timeout)
	default:
		return nil, fmt.Errorf("llm/eino: 不支持的 Provider=%q（可用: openai, ark, ollama）", cfg.Provider)
	}
	if err != nil {
		return nil, err
	}

	return newAdapter(einoM, cfg.Model), nil
}

// ==================== 各 Provider eino 实例创建 ====================

// createOpenAI 创建 OpenAI / 任意 OpenAI-Compatible 的 eino 模型
func createOpenAI(ctx context.Context, cfg *Config, timeout time.Duration) (einoModel.ToolCallingChatModel, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("Provider=openai 时 APIKey 不能为空")
	}
	modelConfig := &einoopenai.ChatModelConfig{
		APIKey:     cfg.APIKey,
		Model:      cfg.Model,
		Timeout:    timeout,
		ByAzure:    cfg.ByAzure,
		BaseURL:    cfg.BaseURL,    // 空=官方；非空=自定义兼容接口
		APIVersion: cfg.APIVersion, // Azure专用
	}
	if cfg.MaxTokens > 0 {
		maxTokens := cfg.MaxTokens
		modelConfig.MaxTokens = &maxTokens
	}
	modelConfig.Temperature = float32Ptr(cfg.Temperature)
	modelConfig.TopP = float32Ptr(cfg.TopP)
	m, err := einoopenai.NewChatModel(ctx, modelConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 OpenAI 模型失败: %w", err)
	}
	return m, nil
}

// createArk 创建火山引擎 Ark 的 eino 模型（字节/豆包）
func createArk(ctx context.Context, cfg *Config, timeout time.Duration) (einoModel.ToolCallingChatModel, error) {
	if cfg.ArkAPIKey == "" && (cfg.ArkAccessKey == "" || cfg.ArkSecretKey == "") {
		return nil, fmt.Errorf("Provider=ark 时需提供 ArkAPIKey 或 ArkAccessKey+ArkSecretKey")
	}
	modelConfig := &einoark.ChatModelConfig{
		APIKey:    cfg.ArkAPIKey,
		AccessKey: cfg.ArkAccessKey,
		SecretKey: cfg.ArkSecretKey,
		Model:     cfg.Model,
		Timeout:   &timeout,
		BaseURL:   cfg.BaseURL,
	}
	if cfg.MaxTokens > 0 {
		maxTokens := cfg.MaxTokens
		modelConfig.MaxTokens = &maxTokens
	}
	modelConfig.Temperature = float32Ptr(cfg.Temperature)
	modelConfig.TopP = float32Ptr(cfg.TopP)
	m, err := einoark.NewChatModel(ctx, modelConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 Ark 模型失败: %w", err)
	}
	return m, nil
}

func float32Ptr(value *float64) *float32 {
	if value == nil {
		return nil
	}
	converted := float32(*value)
	return &converted
}

// createOllama 创建本地 Ollama 的 eino 模型
func createOllama(ctx context.Context, cfg *Config, timeout time.Duration) (einoModel.ToolCallingChatModel, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	m, err := einoollama.NewChatModel(ctx, &einoollama.ChatModelConfig{
		BaseURL: baseURL,
		Model:   cfg.Model,
		Timeout: timeout,
		Options: ollamaOptions(cfg),
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Ollama 模型失败: %w", err)
	}
	return m, nil
}

func ollamaOptions(cfg *Config) *einoollama.Options {
	if cfg.MaxTokens <= 0 && cfg.Temperature == nil && cfg.TopP == nil {
		return nil
	}
	options := &einoollama.Options{NumPredict: cfg.MaxTokens}
	if cfg.Temperature != nil {
		options.Temperature = float32(*cfg.Temperature)
	}
	if cfg.TopP != nil {
		options.TopP = float32(*cfg.TopP)
	}
	return options
}
