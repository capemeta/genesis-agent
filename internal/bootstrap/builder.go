package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"genesis-agent/internal/app"
	connectionfile "genesis-agent/internal/capabilities/connection/adapter/file"
	connectioncontract "genesis-agent/internal/capabilities/connection/contract"
	connectionservice "genesis-agent/internal/capabilities/connection/service"
	credentialfile "genesis-agent/internal/capabilities/credential/adapter/file"
	credentialcontract "genesis-agent/internal/capabilities/credential/contract"
	credentialservice "genesis-agent/internal/capabilities/credential/service"
	llmadapter "genesis-agent/internal/capabilities/llm/adapter"
	"genesis-agent/internal/capabilities/memory/adapter/inmemory"
	memorycontract "genesis-agent/internal/capabilities/memory/contract"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/tool/adapter/builtin"
	"genesis-agent/internal/capabilities/tool/adapter/registry"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/gateway"
	consoletrace "genesis-agent/internal/capabilities/trace/adapter"
	tracecontract "genesis-agent/internal/capabilities/trace/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/config"
	platformhttp "genesis-agent/internal/platform/httpclient"
	loggercontract "genesis-agent/internal/platform/logger"
	sloglogger "genesis-agent/internal/platform/logger"
	promptbuilder "genesis-agent/internal/runtime/prompt"
	"genesis-agent/internal/runtime/strategy/react"
)

const (
	defaultRouteName        = "chat"
	defaultAgentID          = "default-agent"
	defaultAgentDisplayName = "Genesis Agent"
)

// BuildOptions 描述产品无关运行时构建参数。
// 产品分发层只能通过这些显式参数覆盖默认装配行为。
type BuildOptions struct {
	ConfigDir        string
	Quiet            bool
	RouteName        string
	DefaultAgentID   string
	DefaultAgentName string
	Profile          profilemodel.Profile
	AdditionalTools  []toolcontract.Tool
}

// RuntimeBundle 聚合 shared builder 构建出的运行时依赖。
type RuntimeBundle struct {
	Config       *config.Config
	Logger       loggercontract.Logger
	Tracer       tracecontract.Tracer
	ToolRegistry toolcontract.Registry
	MemoryStore  memorycontract.ShortTermStore
	DefaultAgent *domain.Agent
	AgentService app.AgentService
}

// BuildAgentService 构建产品无关的 Agent 运行时服务。
func BuildAgentService(ctx context.Context, opts BuildOptions) (*RuntimeBundle, error) {
	configDir := strings.TrimSpace(opts.ConfigDir)
	if configDir == "" {
		configDir = "configs"
	}
	routeName := strings.TrimSpace(opts.RouteName)
	if routeName == "" {
		routeName = defaultRouteName
	}
	agentID := strings.TrimSpace(opts.DefaultAgentID)
	if agentID == "" {
		agentID = defaultAgentID
	}
	agentName := strings.TrimSpace(opts.DefaultAgentName)
	if agentName == "" {
		agentName = defaultAgentDisplayName
	}
	toolSet := opts.Profile.Tools

	cfg, err := config.Load(configDir)
	if err != nil {
		return nil, fmt.Errorf("%w\n提示: 检查 %s/config.yaml，或通过 AGENT_LLM_API_KEY 注入 API Key", err, configDir)
	}

	var log loggercontract.Logger
	var tracer tracecontract.Tracer
	if opts.Quiet {
		log = sloglogger.NewNop()
		tracer = consoletrace.NewNopTracer()
	} else {
		log = sloglogger.New(sloglogger.ParseLevel(cfg.Log.Level))
		tracer = consoletrace.NewConsoleTracer()
	}

	httpClient := platformhttp.New(
		platformhttp.WithConfig(platformhttp.Config{
			DefaultTimeout:        cfg.HTTPClient.DefaultTimeout,
			ResponseHeaderTimeout: cfg.HTTPClient.ResponseHeaderTimeout,
			TLSHandshakeTimeout:   cfg.HTTPClient.TLSHandshakeTimeout,
			IdleConnTimeout:       cfg.HTTPClient.IdleConnTimeout,
			SSEIdleTimeout:        cfg.HTTPClient.SSEIdleTimeout,
			MaxIdleConns:          cfg.HTTPClient.MaxIdleConns,
			MaxIdleConnsPerHost:   cfg.HTTPClient.MaxIdleConnsPerHost,
			MaxResponseBodyBytes:  cfg.HTTPClient.MaxResponseBodyBytes,
			MaxRequestBodyBytes:   cfg.HTTPClient.MaxRequestBodyBytes,
			MaxErrorBodyBytes:     cfg.HTTPClient.MaxErrorBodyBytes,
			UserAgent:             cfg.HTTPClient.UserAgent,
			RequestIDHeader:       cfg.HTTPClient.RequestIDHeader,
			Retry: platformhttp.RetryPolicy{
				MaxAttempts:    cfg.HTTPClient.Retry.MaxAttempts,
				InitialBackoff: cfg.HTTPClient.Retry.InitialBackoff,
				MaxBackoff:     cfg.HTTPClient.Retry.MaxBackoff,
				Multiplier:     cfg.HTTPClient.Retry.Multiplier,
				Jitter:         cfg.HTTPClient.Retry.Jitter,
			},
		}),
		platformhttp.WithLogger(log),
		platformhttp.WithTracer(tracer),
	)

	credentialSvc, connectionSvc, err := buildSecretServices(configDir, cfg, log)
	if err != nil {
		return nil, err
	}

	baseRegistry := registry.NewRegistry()
	baseRegistry.Register(builtin.NewCurrentTimeTool())
	baseRegistry.Register(builtin.NewCalculatorTool())
	baseRegistry.Register(builtin.NewHTTPRequestTool(httpClient, connectionSvc))
	for _, t := range opts.AdditionalTools {
		if t != nil {
			baseRegistry.Register(t)
		}
	}
	toolGateway := gateway.New(baseRegistry, toolSet)

	resolvedLLM, err := cfg.LLM.ResolveRoute(routeName)
	if err != nil {
		return nil, fmt.Errorf("解析默认 LLM 失败: %w", err)
	}

	llmClient, err := llmadapter.NewChatModelByConfig(ctx, resolvedLLM)
	if err != nil {
		return nil, fmt.Errorf("初始化 LLM 客户端失败: %w", err)
	}

	memStore := inmemory.NewInMemoryStore()
	runEngine := react.NewReactLoopEngine(
		llmClient,
		toolGateway,
		memStore,
		promptbuilder.New(),
		log,
		tracer,
	)

	defaultAgent := &domain.Agent{
		ID:           agentID,
		TenantID:     "dev",
		Name:         agentName,
		Type:         domain.AgentTypeReactLoop,
		DefaultModel: resolvedLLM.Model,
		SystemPrompt: cfg.Agent.SystemPrompt,
		RuntimePolicy: domain.RuntimePolicy{
			MaxIterations: cfg.Agent.MaxIterations,
		},
	}

	svc := app.NewAgentService(cfg, runEngine, memStore, toolGateway, defaultAgent, credentialSvc, connectionSvc)
	return &RuntimeBundle{
		Config:       cfg,
		Logger:       log,
		Tracer:       tracer,
		ToolRegistry: toolGateway,
		MemoryStore:  memStore,
		DefaultAgent: defaultAgent,
		AgentService: svc,
	}, nil
}

func buildSecretServices(configDir string, cfg *config.Config, log loggercontract.Logger) (credentialcontract.Service, connectioncontract.Service, error) {
	dataRoot, err := resolveDataRoot(configDir, cfg.Secrets.DataDir)
	if err != nil {
		return nil, nil, err
	}

	masterKey := strings.TrimSpace(os.Getenv(cfg.Secrets.MasterKeyEnv))
	var credentialSvc credentialcontract.Service
	if masterKey == "" {
		log.Warn("credential store 未启用", "master_key_env", cfg.Secrets.MasterKeyEnv)
		credentialSvc = credentialservice.NewDisabled("请设置环境变量 " + cfg.Secrets.MasterKeyEnv)
	} else {
		store, err := credentialfile.New(dataRoot, masterKey)
		if err != nil {
			return nil, nil, fmt.Errorf("初始化 credential store 失败: %w", err)
		}
		credentialSvc = credentialservice.New(store)
	}

	connectionStore, err := connectionfile.New(dataRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("初始化 connection store 失败: %w", err)
	}
	return credentialSvc, connectionservice.New(connectionStore, credentialSvc), nil
}

func resolveDataRoot(configDir string, dataDir string) (string, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		dataDir = "data"
	}
	if filepath.IsAbs(dataDir) {
		return filepath.Clean(dataDir), nil
	}
	root := filepath.Join(configDir, "..", dataDir)
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("解析 secrets.data_dir 失败: %w", err)
	}
	return abs, nil
}
