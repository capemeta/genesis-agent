package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"genesis-agent/internal/app"
	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	auditmemory "genesis-agent/internal/capabilities/audit/adapter/memory"
	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	connectionfile "genesis-agent/internal/capabilities/connection/adapter/file"
	connectioncontract "genesis-agent/internal/capabilities/connection/contract"
	connectionservice "genesis-agent/internal/capabilities/connection/service"
	credentialfile "genesis-agent/internal/capabilities/credential/adapter/file"
	credentialcontract "genesis-agent/internal/capabilities/credential/contract"
	credentialservice "genesis-agent/internal/capabilities/credential/service"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	hookbuiltin "genesis-agent/internal/capabilities/hook/adapter/builtin"
	hookcommand "genesis-agent/internal/capabilities/hook/adapter/command"
	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	hookservice "genesis-agent/internal/capabilities/hook/service"
	llmadapter "genesis-agent/internal/capabilities/llm/adapter"
	filememory "genesis-agent/internal/capabilities/memory/adapter/file"
	memorycontract "genesis-agent/internal/capabilities/memory/contract"
	memoryservice "genesis-agent/internal/capabilities/memory/service"
	planmemory "genesis-agent/internal/capabilities/plan/adapter/memory"
	plancontract "genesis-agent/internal/capabilities/plan/contract"
	"genesis-agent/internal/capabilities/plan/service"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	subagentmodel "genesis-agent/internal/capabilities/subagent/model"
	subagentservice "genesis-agent/internal/capabilities/subagent/service"
	subagentlifecycle "genesis-agent/internal/capabilities/subagent/tool/lifecycle"
	subagenttask "genesis-agent/internal/capabilities/subagent/tool/task"
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
	runtimecontext "genesis-agent/internal/runtime/context"
	"genesis-agent/internal/runtime/multiagent/controller"
	promptbuilder "genesis-agent/internal/runtime/prompt"
	"genesis-agent/internal/runtime/strategy/react"

	progressbc "genesis-agent/internal/capabilities/plan/adapter/progress"
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
	Authorizer                gateway.Authorizer
	HookExecutionRunner       execcontract.ExecutionRunner
	HookApproval              approvalcontract.Service
	SubAgentApproval          approvalcontract.Service
	// SubAgentMaxConcurrent 是根会话内的子智能体并发硬限；产品在 bootstrap 注入默认值。
	SubAgentMaxConcurrent int
}

// RuntimeBundle 聚合 shared builder 构建出的运行时依赖。
type RuntimeBundle struct {
	Config       *config.Config
	Logger       loggercontract.Logger
	Tracer       tracecontract.Tracer
	ToolGateway  *gateway.Gateway
	ToolRegistry toolcontract.Registry
	MemoryStore  memorycontract.ShortTermMemory
	AuditSink    auditcontract.Sink
	UsageSink    usagecontract.Sink
	DefaultAgent *domain.Agent
	AgentService app.AgentService
	Credentials  credentialcontract.Service
	Connections  connectioncontract.Service
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
		return nil, fmt.Errorf("%w\n提示: 检查 %s/config.yaml 与 llm.yaml，或通过 AGENT_LLM_PROVIDERS_<PROVIDER>_AUTH_API_KEY 注入 API Key", err, configDir)
	}
	hookConfig, err := config.LoadHookConfig(configDir, product)
	if err != nil {
		return nil, fmt.Errorf("加载 Hook 配置失败: %w", err)
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

	// ── 初始化 Plan 应用服务 ───────────────────
	planRepo := opts.PlanRepository
	if planRepo == nil {
		planRepo = planmemory.New()
	}
	// ProgressBroadcaster 把计划变更转为 progress.Event{Kind: KindPlan}，
	// CLI TUI / Desktop / Enterprise 各端均可通过订阅 progress 流实时渲染计划卡片。
	planBroadcaster := progressbc.New()
	planSvc := service.NewPlanService(planRepo, planBroadcaster, 3)

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
	toolGateway := gateway.New(baseRegistry, toolSet, gateway.Options{
		Locker:     scheduler.NewMemoryResourceLocker(),
		Tracer:     tracer,
		AuditSink:  auditSink,
		UsageSink:  usageSink,
		Authorizer: opts.Authorizer,
		Approval:   opts.HookApproval,
	})

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

	// 会话历史与日志目录完全解耦，固定保存在项目级工作区的 .genesis/sessions 目录下
	sessionDir := filepath.Join(".", ".genesis", "sessions")

	estimator := runtimecontext.NewHeuristicEstimator()

	// 转换配置 Profile DTO 映射
	customProfiles := make(map[string]runtimecontext.ContextProfile)
	for k, p := range cfg.ContextProfiles {
		customProfiles[k] = runtimecontext.ConvertProfileConfig(p.Weights, p.Clamp)
	}
	planner := runtimecontext.NewContextBudgetPlanner(customProfiles)

	fileMem := filememory.NewFileShortTermMemory(sessionDir, estimator, llmClient)
	planSvc.SetShortTermMemory(fileMem)
	memStore := fileMem

	// 长期记忆资产与用户画像资产目录解耦与结构化归档
	// 1. 长期记忆强制固定在工作区根目录的 .genesis/memories 目录中，避免根目录杂乱
	projectMemoryDir := filepath.Join(".", ".genesis", "memories")
	ltm := filememory.NewFileLongTermMemory(projectMemoryDir)

	// 2. 用户画像为用户全局资产，存放于用户主目录 ~/.genesis-agent/memories 下，实现多项目全局共享
	homeDir, _ := os.UserHomeDir()
	globalProfileDir := filepath.Join(homeDir, ".genesis-agent", "memories")
	userProfileStore := filememory.NewFileUserProfileStore(globalProfileDir)

	memExtractor := memoryservice.NewDefaultMemoryExtractor(llmClient)
	worker := memoryservice.NewMemoryExtractWorker(memExtractor, ltm)
	worker.Start(ctx)

	compactor := runtimecontext.NewDefaultCompactor(
		estimator,
		fileMem,
		fileMem,
		sessionDir,
		worker,
		resolvedLLM.ContextWindow,
		6,    // keepRecentTurns 默认保留 6 轮
		0,    // keepRecentTokenBudget
		0.85, // compactRatio 默认 85% 水位触发
		0.75, // warnRatio 75%
		8000, // toolResultMaxTokens
	)

	effectiveContextRatio := 0.92
	if resolvedLLM.EffectiveContextRatio != nil {
		effectiveContextRatio = *resolvedLLM.EffectiveContextRatio
	}
	runEngine := react.NewReactLoopEngine(
		llmClient,
		toolGateway,
		memStore,
		promptbuilder.New(injectors...),
		log,
		tracer,
		estimator,
		planner,
		resolvedLLM.ContextWindow,
		resolvedLLM.MaxTokens,
		react.WithSkillNameMatcher(opts.SkillNameMatcher),
		react.WithSkillMentionSelector(opts.SkillMentionSelector),
		react.WithSkillExplicitLoader(opts.SkillExplicitLoader),
		react.WithCompactor(compactor),
		react.WithLongTermMemory(ltm),
		react.WithUserProfileStore(userProfileStore),
		react.WithContextBudgetConfig(effectiveContextRatio, resolvedLLM.OutputReserveTokens),
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

	// 子智能体内核与三端共享；当前使用内存槽位，不引入 DB/分布式后端。
	maxConcurrent := opts.SubAgentMaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	limiter, err := controller.NewMemorySlotLimiter(maxConcurrent)
	if err != nil {
		return nil, fmt.Errorf("初始化 subagent 并发槽失败: %w", err)
	}
	subagentController, err := controller.New(runEngine, limiter, log)
	if err != nil {
		return nil, fmt.Errorf("初始化 subagent Controller 失败: %w", err)
	}
	workspace, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("读取工作区失败: %w", err)
	}
	projectDefinitions, err := subagentservice.LoadProjectDefinitions(workspace)
	if err != nil {
		return nil, err
	}
	builtinCatalog := subagentservice.NewBuiltinCatalog()
	builtinDefinitions := make([]subagentmodel.Definition, 0, len(builtinCatalog.List()))
	for _, summary := range builtinCatalog.List() {
		definition, _ := builtinCatalog.Get(summary.Name)
		builtinDefinitions = append(builtinDefinitions, definition)
	}
	taskTool, err := subagenttask.New(subagenttask.Deps{
		Catalog:      subagentservice.NewMergedCatalog(builtinDefinitions, projectDefinitions),
		Controller:   subagentController,
		BaseAgent:    defaultAgent,
		AllowedTools: append([]string(nil), toolSet.Enabled...),
		Approval:     opts.SubAgentApproval,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 Task 工具失败: %w", err)
	}
	toolGateway.Register(taskTool)
	taskOutputTool, taskStopTool, err := subagentlifecycle.New(subagentController)
	if err != nil {
		return nil, fmt.Errorf("初始化 Task 生命周期工具失败: %w", err)
	}
	toolGateway.Register(taskOutputTool)
	toolGateway.Register(taskStopTool)

	builtinHooks := hookbuiltin.NewDefaultRegistry()
	hookRunners := []hookcontract.Runner{builtinHooks}
	if opts.HookExecutionRunner != nil {
		commandRunner, commandErr := hookcommand.NewRunner(opts.HookExecutionRunner, hookConfig.DefaultTimeout)
		if commandErr != nil {
			return nil, commandErr
		}
		hookRunners = append(hookRunners, commandRunner)
	}
	hookDispatcher := hookservice.NewDispatcherWithOptions(hookConfig, []hookservice.DispatcherOption{hookservice.WithAuditSink(auditSink), hookservice.WithTracer(tracer), hookservice.WithDefaultScope(hookScopeForProduct(product))}, hookRunners...)
	svc := app.NewAgentService(cfg, runEngine, memStore, fileMem, toolGateway, defaultAgent, credentialSvc, connectionSvc, ltm, userProfileStore, hookDispatcher)
	return &RuntimeBundle{
		Config:       cfg,
		Logger:       log,
		Tracer:       tracer,
		ToolGateway:  toolGateway,
		ToolRegistry: toolGateway,
		MemoryStore:  memStore,
		AuditSink:    auditSink,
		UsageSink:    usageSink,
		DefaultAgent: defaultAgent,
		AgentService: svc,
		Credentials:  credentialSvc,
		Connections:  connectionSvc,
	}, nil
}

func hookScopeForProduct(product string) hookmodel.ScopeContext {
	scope := hookmodel.ScopeContext{Channel: product}
	switch product {
	case "enterprise":
		scope.Environment = "server"
	case "desktop":
		scope.Environment = "desktop"
	default:
		scope.Environment = "local"
	}
	return scope
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
