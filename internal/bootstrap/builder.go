package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"genesis-agent/internal/app"
	auditmemory "genesis-agent/internal/capabilities/audit/adapter/memory"
	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	connectionfile "genesis-agent/internal/capabilities/connection/adapter/file"
	connectioncontract "genesis-agent/internal/capabilities/connection/contract"
	connectionservice "genesis-agent/internal/capabilities/connection/service"
	credentialfile "genesis-agent/internal/capabilities/credential/adapter/file"
	credentialcontract "genesis-agent/internal/capabilities/credential/contract"
	credentialservice "genesis-agent/internal/capabilities/credential/service"
	llmadapter "genesis-agent/internal/capabilities/llm/adapter"
	"genesis-agent/internal/capabilities/memory/adapter/inmemory"
	memorycontract "genesis-agent/internal/capabilities/memory/contract"
	planmemory "genesis-agent/internal/capabilities/plan/adapter/memory"
	plancontract "genesis-agent/internal/capabilities/plan/contract"
	"genesis-agent/internal/capabilities/plan/service"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/tool/adapter/builtin"
	"genesis-agent/internal/capabilities/tool/adapter/registry"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/gateway"
	"genesis-agent/internal/capabilities/tool/scheduler"
	consoletrace "genesis-agent/internal/capabilities/trace/adapter"
	tracecontract "genesis-agent/internal/capabilities/trace/contract"
	usagememory "genesis-agent/internal/capabilities/usage/adapter/memory"
	usagecontract "genesis-agent/internal/capabilities/usage/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/config"
	"genesis-agent/internal/platform/contextutil"
	platformhttp "genesis-agent/internal/platform/httpclient"
	loggercontract "genesis-agent/internal/platform/logger"
	sloglogger "genesis-agent/internal/platform/logger"
	promptbuilder "genesis-agent/internal/runtime/prompt"
	"genesis-agent/internal/runtime/strategy/react"

	webcontract "genesis-agent/internal/capabilities/web/contract"
	webfetchtool "genesis-agent/internal/capabilities/web/tool/web_fetch"
	websearchtool "genesis-agent/internal/capabilities/web/tool/web_search"
)

const (
	defaultRouteName        = "chat"
	defaultAgentID          = "default-agent"
	defaultAgentDisplayName = "Genesis Agent"
)

// WebBuildOptions 描述 web_search 和 web_fetch 的构建参数
type WebBuildOptions struct {
	Enabled        bool
	Searcher       webcontract.Searcher
	Fetcher        webcontract.FetchService
	RegisterSearch bool
	RegisterFetch  bool
}

// BuildOptions 描述产品无关运行时构建参数。
// 产品分发层只能通过这些显式参数覆盖默认装配行为。
type BuildOptions struct {
	Product          string
	ConfigDir        string
	Quiet            bool
	RouteName        string
	DefaultAgentID   string
	DefaultAgentName string
	Profile          profilemodel.Profile
	AdditionalTools  []toolcontract.Tool
	PromptInjectors  []promptbuilder.ContextInjector
	// Logger 由产品层注入；非 nil 时 builder 不再自建文件日志（禁止双 Writer）。
	Logger                    loggercontract.Logger
	AuditSink                 auditcontract.Sink
	UsageSink                 usagecontract.Sink
	Web                       WebBuildOptions
	PlanRepository            plancontract.Repository
	SkillNameMatcher          react.SkillNameMatcher
	SkillMentionSelector      react.SkillMentionSelector
	SkillExplicitLoader       react.SkillExplicitLoader
	AutoRewriteSkillCollision *bool
}

// RuntimeBundle 聚合 shared builder 构建出的运行时依赖。
type RuntimeBundle struct {
	Config       *config.Config
	Logger       loggercontract.Logger
	Tracer       tracecontract.Tracer
	ToolRegistry toolcontract.Registry
	MemoryStore  memorycontract.ShortTermStore
	AuditSink    auditcontract.Sink
	UsageSink    usagecontract.Sink
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

	product := strings.TrimSpace(opts.Product)
	if product == "" {
		product = "cli"
	}
	cfg, err := config.LoadForProduct(configDir, product)
	if err != nil {
		return nil, fmt.Errorf("%w\n提示: 检查 %s/config.yaml，或通过 AGENT_LLM_API_KEY 注入 API Key", err, configDir)
	}

	var log loggercontract.Logger
	var tracer tracecontract.Tracer
	if opts.Logger != nil {
		log = opts.Logger
	} else if opts.Quiet {
		// 产品层未注入 Logger 时 Quiet 仅用 Nop，避免与产品 RuntimeLogging 双开文件。
		log = sloglogger.NewNop()
	} else {
		log = sloglogger.New(sloglogger.ParseLevel(cfg.Log.Level))
	}
	if opts.Quiet {
		tracer = consoletrace.NewNopTracer()
	} else {
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

	// ── 初始化 Plan 应用服务 ───────────────────────────
	planRepo := opts.PlanRepository
	if planRepo == nil {
		planRepo = planmemory.New()
	}
	planSvc := service.NewPlanService(planRepo, nil, 3)

	baseRegistry := registry.NewRegistry()
	baseRegistry.Register(builtin.NewCurrentTimeTool())
	baseRegistry.Register(builtin.NewCalculatorTool())
	baseRegistry.Register(builtin.NewHTTPRequestTool(httpClient, connectionSvc))
	baseRegistry.Register(builtin.NewTodoReadTool(planSvc))
	baseRegistry.Register(builtin.NewTodoWriteTool(planSvc))
	baseRegistry.Register(builtin.NewTodoUpdateStepTool(planSvc))

	if opts.Web.Enabled {
		if opts.Web.RegisterSearch {
			if opts.Web.Searcher == nil {
				return nil, fmt.Errorf("RegisterSearch is true but Searcher is nil")
			}
			searchTool, err := websearchtool.New(opts.Web.Searcher)
			if err != nil {
				return nil, fmt.Errorf("failed to create web_search tool: %w", err)
			}
			baseRegistry.Register(searchTool)
		}
		if opts.Web.RegisterFetch {
			if opts.Web.Fetcher == nil {
				return nil, fmt.Errorf("RegisterFetch is true but Fetcher is nil")
			}
			fetchTool, err := webfetchtool.New(opts.Web.Fetcher)
			if err != nil {
				return nil, fmt.Errorf("failed to create web_fetch tool: %w", err)
			}
			baseRegistry.Register(fetchTool)
		}
	}

	for _, t := range opts.AdditionalTools {
		if t != nil {
			baseRegistry.Register(t)
		}
	}
	auditSink := opts.AuditSink
	if auditSink == nil {
		auditSink = auditmemory.NewSink()
	}
	usageSink := opts.UsageSink
	if usageSink == nil {
		usageSink = usagememory.NewSink()
	}
	toolGateway := gateway.New(baseRegistry, toolSet, gateway.Options{Locker: scheduler.NewMemoryResourceLocker(), Tracer: tracer, AuditSink: auditSink, UsageSink: usageSink})

	resolvedLLM, err := cfg.LLM.ResolveRoute(routeName)
	if err != nil {
		return nil, fmt.Errorf("解析默认 LLM 失败: %w", err)
	}

	llmClient, err := llmadapter.NewChatModelByConfig(ctx, resolvedLLM)
	if err != nil {
		return nil, fmt.Errorf("初始化 LLM 客户端失败: %w", err)
	}

	// 动态注入 Plan 待办进度被动提醒
	planReminderInjector := promptbuilder.ContextInjectorFunc(func(c context.Context, req promptbuilder.BuildRequest) (promptbuilder.Fragment, error) {
		sessionID := req.Run.SessionID
		if sessionID == "" {
			sessionID = req.Run.ID
		}
		stepCount := len(req.Run.Steps)

		c = contextutil.WithSessionID(c, sessionID)
		c = contextutil.WithTenantID(c, req.Run.TenantID)

		reminder, needed, err := planSvc.GeneratePromptReminder(c, sessionID, stepCount)
		if err != nil {
			return promptbuilder.Fragment{}, err
		}
		if !needed {
			return promptbuilder.Fragment{}, nil
		}

		return promptbuilder.Fragment{
			Name:     "plan_reminder",
			Contents: reminder,
		}, nil
	})

	injectors := append([]promptbuilder.ContextInjector{planReminderInjector}, opts.PromptInjectors...)

	memStore := inmemory.NewInMemoryStore()
	runEngine := react.NewReactLoopEngine(
		llmClient,
		toolGateway,
		memStore,
		promptbuilder.New(injectors...),
		log,
		tracer,
		react.WithSkillNameMatcher(opts.SkillNameMatcher),
		react.WithSkillMentionSelector(opts.SkillMentionSelector),
		react.WithSkillExplicitLoader(opts.SkillExplicitLoader),
		func() react.EngineOption {
			if opts.AutoRewriteSkillCollision == nil {
				return nil
			}
			return react.WithAutoRewriteSkillCollision(*opts.AutoRewriteSkillCollision)
		}(),
	)

	defaultAgent := &domain.Agent{
		ID:           agentID,
		TenantID:     "dev",
		Name:         agentName,
		Type:         domain.AgentTypeReactLoop,
		DefaultModel: resolvedLLM.Model,
		SystemPrompt: cfg.Agent.SystemPrompt,
		RuntimePolicy: domain.RuntimePolicy{
			MaxIterations:            cfg.Agent.MaxIterations,
			MaxConsecutiveFail:       cfg.Agent.MaxConsecutiveFail,
			RepeatGuardEnabled:       cfg.Agent.RepeatGuardEnabled,
			MaxIdenticalToolFailures: cfg.Agent.MaxIdenticalToolFailures,
			MaxStagnantIterations:    cfg.Agent.MaxStagnantIterations,
		},
	}

	svc := app.NewAgentService(cfg, runEngine, memStore, toolGateway, defaultAgent, credentialSvc, connectionSvc)
	return &RuntimeBundle{
		Config:       cfg,
		Logger:       log,
		Tracer:       tracer,
		ToolRegistry: toolGateway,
		MemoryStore:  memStore,
		AuditSink:    auditSink,
		UsageSink:    usageSink,
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
