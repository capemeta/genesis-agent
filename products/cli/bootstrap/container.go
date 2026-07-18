// Package bootstrap 装配 Genesis CLI 产品入口。
package bootstrap

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"genesis-agent/internal/app"
	shared "genesis-agent/internal/bootstrap"
	agentappmemory "genesis-agent/internal/capabilities/agentapp/adapter/memory"
	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	approvalmemory "genesis-agent/internal/capabilities/approval/adapter/memory"
	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalservice "genesis-agent/internal/capabilities/approval/service"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	auditfile "genesis-agent/internal/capabilities/audit/adapter/file"
	auditmemory "genesis-agent/internal/capabilities/audit/adapter/memory"
	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	capcontract "genesis-agent/internal/capabilities/capability/contract"
	capservice "genesis-agent/internal/capabilities/capability/service"
	execsandbox "genesis-agent/internal/capabilities/execution/adapter/sandbox"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	execservice "genesis-agent/internal/capabilities/execution/service"
	runcommand "genesis-agent/internal/capabilities/execution/tool/run_command"
	writestdin "genesis-agent/internal/capabilities/execution/tool/write_stdin"
	"genesis-agent/internal/capabilities/filesystem/freshness"
	fspermission "genesis-agent/internal/capabilities/filesystem/permission"
	applypatchtool "genesis-agent/internal/capabilities/filesystem/tool/apply_patch"
	editfile "genesis-agent/internal/capabilities/filesystem/tool/edit_file"
	globtool "genesis-agent/internal/capabilities/filesystem/tool/glob"
	greptool "genesis-agent/internal/capabilities/filesystem/tool/grep"
	listdir "genesis-agent/internal/capabilities/filesystem/tool/list_dir"
	readfile "genesis-agent/internal/capabilities/filesystem/tool/read_file"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	walkdir "genesis-agent/internal/capabilities/filesystem/tool/walk_dir"
	writefile "genesis-agent/internal/capabilities/filesystem/tool/write_file"
	mcpstore "genesis-agent/internal/capabilities/mcp/adapter/store"
	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
	mcpstack "genesis-agent/internal/capabilities/mcp/stack"
	policyapproval "genesis-agent/internal/capabilities/policy/adapter/approval"
	policyconfig "genesis-agent/internal/capabilities/policy/adapter/config"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	sandboxhttp "genesis-agent/internal/capabilities/sandbox/adapter/http"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	skillembedded "genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcollision "genesis-agent/internal/capabilities/skill/collision"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
	scriptservice "genesis-agent/internal/capabilities/skill/script/service"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	installskilldeps "genesis-agent/internal/capabilities/skill/tool/install_skill_dependencies"
	installskillfromsource "genesis-agent/internal/capabilities/skill/tool/install_skill_from_source"
	listskillresources "genesis-agent/internal/capabilities/skill/tool/list_skill_resources"
	readskillresource "genesis-agent/internal/capabilities/skill/tool/read_skill_resource"
	runskillcommand "genesis-agent/internal/capabilities/skill/tool/run_skill_command"
	searchskillresources "genesis-agent/internal/capabilities/skill/tool/search_skill_resources"
	skilltool "genesis-agent/internal/capabilities/skill/tool/skill"
	toolcapability "genesis-agent/internal/capabilities/tool/adapter/capability"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
	toolvalidation "genesis-agent/internal/capabilities/tool/validation"
	usagefile "genesis-agent/internal/capabilities/usage/adapter/file"
	usagememory "genesis-agent/internal/capabilities/usage/adapter/memory"
	usagecontract "genesis-agent/internal/capabilities/usage/contract"
	workspaceadapter "genesis-agent/internal/capabilities/workspace/adapter/sandbox"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	workservice "genesis-agent/internal/capabilities/workspace/service"
	platformconfig "genesis-agent/internal/platform/config"
	"genesis-agent/internal/platform/idgen"
	"genesis-agent/internal/platform/logger"
	multicontract "genesis-agent/internal/runtime/multiagent/contract"
	multiagentmodel "genesis-agent/internal/runtime/multiagent/model"
	multiprojection "genesis-agent/internal/runtime/multiagent/projection"
	promptbuilder "genesis-agent/internal/runtime/prompt"
	"genesis-agent/internal/runtime/strategy/react"
	cliapproval "genesis-agent/products/cli/internal/approval"
	"genesis-agent/products/cli/internal/command"
	"genesis-agent/products/cli/internal/profile"
	clisandbox "genesis-agent/products/cli/internal/sandbox"
	cliskill "genesis-agent/products/cli/internal/skill"
	clisubagent "genesis-agent/products/cli/internal/subagent"
	"genesis-agent/products/cli/internal/tui"
	localartifactcontrol "genesis-agent/shared/local/artifactcontrol"
	localexec "genesis-agent/shared/local/execution"
	localfs "genesis-agent/shared/local/filesystem"
	localresolver "genesis-agent/shared/local/pathresolver"
	localplan "genesis-agent/shared/local/plan"
	windowssandbox "genesis-agent/shared/local/sandbox/windows"
	localskill "genesis-agent/shared/local/skill"
	localworkspace "genesis-agent/shared/local/workspace"

	httpfetcher "genesis-agent/internal/capabilities/web/adapter/fetch/http"
	ddg "genesis-agent/internal/capabilities/web/adapter/search/fallback/duckduckgo"
	brave "genesis-agent/internal/capabilities/web/adapter/search/international/brave"
	exa "genesis-agent/internal/capabilities/web/adapter/search/international/exa"
	serpapi "genesis-agent/internal/capabilities/web/adapter/search/international/serpapi"
	tavily "genesis-agent/internal/capabilities/web/adapter/search/international/tavily"
	searxng "genesis-agent/internal/capabilities/web/adapter/search/self_hosted/searxng"
	webcache "genesis-agent/internal/capabilities/web/cache/memory"
	webcontract "genesis-agent/internal/capabilities/web/contract"
	webextractor "genesis-agent/internal/capabilities/web/extractor/htmlmarkdown"
	webservice "genesis-agent/internal/capabilities/web/service"
)

// Container 是 CLI 产品的装配容器。
type Container struct {
	configDirRef  *string
	quiet         bool
	sandbox       clisandbox.Config
	workspaceRoot string

	once           sync.Once
	initErr        error
	bundle         *shared.RuntimeBundle
	logging        *logger.RuntimeLogging
	mcpStack       *mcpstack.Stack
	mcpStore       contract.ApprovalStore
	runtimeClosers []runtimeCloser
}

type runtimeCloser interface {
	Close(ctx context.Context) error
}

type productRuntime struct {
	tools                []toolcontract.Tool
	promptInjectors      []promptbuilder.ContextInjector
	logger               logger.Logger
	logging              *logger.RuntimeLogging
	auditSink            auditcontract.Sink
	usageSink            usagecontract.Sink
	skillNameMatcher     react.SkillNameMatcher
	skillMentionSelector react.SkillMentionSelector
	skillExplicitLoader  react.SkillExplicitLoader
	capabilityRegistry   capcontract.Registry
	adapterRegistry      capcontract.RuntimeAdapterRegistry
	approvalService      approvalcontract.Service
	hookExecutionRunner  execcontract.ExecutionRunner
	config               *platformconfig.Config
	requestInputs        workcontract.RequestInputPlanner
	runInputs            workcontract.RunInputBinder
	workspaceRoot        string
	artifactRuns         artifactcontract.RunInitializer
	completion           artifactcontract.CompletionPolicy
	qaEvidence           artifactcontract.QAEvidenceRecorder
	runResources         []workcontract.RunResourceReleaser
	runtimeClosers       []runtimeCloser
}

// Execute 执行 CLI 产品命令树。
func Execute(ctx context.Context) error {
	return command.ExecuteWithFactories(
		func(runCtx context.Context, opts command.ServiceOptions) (command.ServiceHandle, error) {
			if runCtx == nil {
				runCtx = ctx
			}
			return openService(runCtx, opts)
		},
		func(runCtx context.Context, opts command.ServiceOptions) (command.MCPAdmin, error) {
			if runCtx == nil {
				runCtx = ctx
			}
			return OpenMCPAdmin(runCtx, opts)
		},
	)
}

// NewContainer 创建 CLI 产品容器。
func NewContainer(configDirRef *string, quiet bool) *Container {
	return &Container{configDirRef: configDirRef, quiet: quiet, workspaceRoot: workspaceRootOrDot("")}
}

// Init 初始化 CLI 产品运行时依赖。
func (c *Container) Init(ctx context.Context) error {
	c.once.Do(func() {
		configDir := ""
		if c.configDirRef != nil {
			configDir = *c.configDirRef
		}
		cfg, err := platformconfig.LoadWithOptions(configDir, platformconfig.LoadOptions{Product: "cli", EnsureUserConfig: true})
		if err != nil {
			c.initErr = fmt.Errorf("加载CLI产品配置失败: %w", err)
			return
		}
		sandboxCfg, err := clisandbox.FromRuntimeConfig(cfg.Sandbox)
		if err != nil {
			c.initErr = fmt.Errorf("解析CLI sandbox配置失败: %w", err)
			return
		}
		if isSandboxOverride(c.sandbox) && !cfg.Sandbox.AllowSessionOverride {
			c.initErr = fmt.Errorf("当前配置不允许会话级 sandbox 覆盖")
			return
		}
		sandboxCfg = clisandbox.MergeSessionOverride(sandboxCfg, c.sandbox)
		c.sandbox = sandboxCfg
		prof := profile.DefaultProfile(cfg.MCP.Enabled)
		runtime, log, err := buildProductRuntime(ctx, configDir, cfg, c.quiet, sandboxCfg, prof, workspaceRootOrDot(c.workspaceRoot))
		if err != nil {
			c.initErr = err
			return
		}
		c.runtimeClosers = append([]runtimeCloser(nil), runtime.runtimeClosers...)
		c.logging = runtime.logging
		webOpts, err := buildWebOptions(cfg, log)
		if err != nil {
			_ = c.logging.Close()
			c.logging = nil
			c.initErr = err
			return
		}
		planRepoDir := filepath.Join(filepath.Dir(configDir), ".genesis", "runtime", "plans")
		planRepo, err := localplan.NewFileRepository(planRepoDir)
		if err != nil {
			_ = c.logging.Close()
			c.logging = nil
			c.initErr = fmt.Errorf("初始化CLI Plan本地存储失败: %w", err)
			return
		}
		subAgentStore, err := clisubagent.NewFileStore(runtime.workspaceRoot)
		if err != nil {
			_ = c.logging.Close()
			c.logging = nil
			c.initErr = err
			return
		}
		subAgentResources, err := clisubagent.NewWorkspaceResources(runtime.workspaceRoot)
		if err != nil {
			_ = c.logging.Close()
			c.logging = nil
			c.initErr = err
			return
		}
		runWorkspace, err := buildLocalRunWorkspace(runtime.workspaceRoot, runtime.requestInputs, runtime.runInputs, runtime.artifactRuns, runtime.completion, runtime.qaEvidence, runtime.runResources)
		if err != nil {
			_ = c.logging.Close()
			c.logging = nil
			c.initErr = fmt.Errorf("初始化 CLI Run 工作空间失败: %w", err)
			return
		}
		c.bundle, c.initErr = shared.BuildAgentService(ctx, shared.BuildOptions{
			Product:                        "cli",
			ConfigDir:                      configDir,
			Quiet:                          c.quiet,
			RouteName:                      "chat",
			DefaultAgentID:                 "default-agent",
			DefaultAgentName:               "Genesis Agent",
			Profile:                        prof,
			RunWorkspace:                   runWorkspace,
			AdditionalTools:                runtime.tools,
			PromptInjectors:                runtime.promptInjectors,
			Logger:                         runtime.logger,
			AuditSink:                      runtime.auditSink,
			UsageSink:                      runtime.usageSink,
			Web:                            webOpts,
			PlanRepository:                 planRepo,
			SkillNameMatcher:               runtime.skillNameMatcher,
			SkillMentionSelector:           runtime.skillMentionSelector,
			SkillExplicitLoader:            runtime.skillExplicitLoader,
			SubAgentMaxConcurrent:          3,
			SubAgentStore:                  subAgentStore,
			SubAgentDelivery:               subAgentStore,
			SubAgentProjection:             multiprojection.NewMemorySink(multiagentmodel.ProjectionChannelCLI),
			SubAgentIncludeUserDefinitions: true,
			SubAgentEvidence:               subAgentResources,
			SubAgentResources:              subAgentResources,
			SubAgentCapabilityRegistry:     runtime.capabilityRegistry,
			SubAgentApproval:               runtime.approvalService,
			HookExecutionRunner:            runtime.hookExecutionRunner,
			HookApproval:                   runtime.approvalService,
		})
		if c.initErr != nil {
			_ = c.logging.Close()
			c.logging = nil
			return
		}
		mcpStack, mcpStore, err := attachMCPStack(ctx, c.bundle, runtime, profilemodel.ChannelCLI, profilemodel.EnvironmentLocal)
		if err != nil {
			_ = c.logging.Close()
			c.logging = nil
			c.bundle = nil
			c.initErr = err
			return
		}
		c.mcpStack = mcpStack
		c.mcpStore = mcpStore
		if err := toolvalidation.ValidateEnabled(c.bundle.ToolRegistry, prof.Tools.Enabled); err != nil {
			if c.mcpStack != nil {
				_ = c.mcpStack.Close(context.Background())
				c.mcpStack = nil
			}
			_ = c.logging.Close()
			c.logging = nil
			c.bundle = nil
			c.initErr = fmt.Errorf("CLI 工具装配与 Profile 不一致: %w", err)
			return
		}
	})
	return c.initErr
}

// Close 释放 MCP 连接与运行日志。
func (c *Container) Close() error {
	var first error
	for index := len(c.runtimeClosers) - 1; index >= 0; index-- {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := c.runtimeClosers[index].Close(closeCtx)
		cancel()
		if err != nil && first == nil {
			first = err
		}
	}
	c.runtimeClosers = nil
	if c.mcpStack != nil {
		if err := c.mcpStack.Close(context.Background()); err != nil && first == nil {
			first = err
		}
		c.mcpStack = nil
	}
	if c.logging != nil {
		if err := c.logging.Close(); err != nil {
			if first == nil {
				first = err
			}
		}
		c.logging = nil
	}
	return first
}

func isSandboxOverride(cfg clisandbox.Config) bool {
	return cfg.Mode != "" ||
		cfg.Execution != "" ||
		cfg.Endpoint != "" ||
		cfg.APIKey != "" ||
		cfg.WorkspaceID != "" ||
		cfg.DefaultRuntimeProfile != ""
}

// Service 返回初始化后的 AgentService。
func (c *Container) Service() app.AgentService {
	if c.bundle == nil {
		return nil
	}
	return c.bundle.AgentService
}

// MCPStack 返回已装配的 MCP 栈（可能为 nil 或空栈）。
func (c *Container) MCPStack() *mcpstack.Stack { return c.mcpStack }

// MCPApprovalStore 返回 project MCP 预连接审批存储。
func (c *Container) MCPApprovalStore() contract.ApprovalStore { return c.mcpStore }

// SubAgentProjectionReader 为 CLI 摘要展示提供安全的控制面事件查询。
func (c *Container) SubAgentProjectionReader() multicontract.ProjectionReader {
	if c == nil || c.bundle == nil {
		return nil
	}
	reader, _ := c.bundle.SubAgentProjection.(multicontract.ProjectionReader)
	return reader
}

// openService 初始化并返回必须关闭的 CLI 服务运行时。
func openService(ctx context.Context, opts command.ServiceOptions) (command.ServiceHandle, error) {
	c := &Container{configDirRef: opts.ConfigDirRef, quiet: opts.Quiet, sandbox: opts.Sandbox, workspaceRoot: workspaceRootOrDot(opts.WorkspaceRoot)}
	if err := c.Init(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// OpenMCPAdmin 初始化 CLI 容器并返回 MCP 管理面句柄（list/approve/refresh）。
func OpenMCPAdmin(ctx context.Context, opts command.ServiceOptions) (command.MCPAdmin, error) {
	c := &Container{configDirRef: opts.ConfigDirRef, quiet: opts.Quiet, sandbox: opts.Sandbox, workspaceRoot: workspaceRootOrDot(opts.WorkspaceRoot)}
	if err := c.Init(ctx); err != nil {
		return nil, err
	}
	return &mcpAdmin{c: c}, nil
}

type mcpAdmin struct{ c *Container }

func (a *mcpAdmin) Close() error {
	if a == nil || a.c == nil {
		return nil
	}
	return a.c.Close()
}

func (a *mcpAdmin) Enabled() bool {
	return a != nil && a.c != nil && a.c.mcpStack != nil && a.c.mcpStack.Manager != nil
}

func (a *mcpAdmin) States() []model.ServerState {
	if !a.Enabled() {
		return nil
	}
	return a.c.mcpStack.Manager.States()
}

func (a *mcpAdmin) Refresh(ctx context.Context) error {
	if !a.Enabled() {
		return fmt.Errorf("MCP 未启用或未装配")
	}
	return a.c.mcpStack.Refresh(ctx)
}

func (a *mcpAdmin) ApprovalStore() contract.ApprovalStore {
	if a == nil || a.c == nil {
		return nil
	}
	return a.c.mcpStore
}

func buildProductRuntime(ctx context.Context, configDir string, cfg *platformconfig.Config, quiet bool, sandboxCfg clisandbox.Config, prof profilemodel.Profile, workspaceRoot string) (productRuntime, logger.Logger, error) {
	if cfg == nil {
		loaded, err := platformconfig.LoadWithOptions(configDir, platformconfig.LoadOptions{Product: "cli", EnsureUserConfig: true})
		if err != nil {
			return productRuntime{}, nil, fmt.Errorf("加载CLI产品配置失败: %w", err)
		}
		cfg = loaded
	}
	runtimeLogging, err := logger.NewRuntimeLogging(cfg.Log, logger.RuntimeLoggingOptions{
		ConfigDir: configDir,
		Quiet:     quiet,
	})
	if err != nil {
		return productRuntime{}, nil, fmt.Errorf("初始化运行日志失败: %w", err)
	}
	log := runtimeLogging.AgentLogger

	var auditSink auditcontract.Sink
	if runtimeLogging.AuditWriter != nil {
		auditSink = auditfile.NewSink(runtimeLogging.AuditWriter)
	} else {
		auditSink = auditmemory.NewSink()
	}
	var usageSink usagecontract.Sink
	if runtimeLogging.UsageWriter != nil {
		usageSink = usagefile.NewSink(runtimeLogging.UsageWriter)
	} else {
		usageSink = usagememory.NewSink()
	}
	baseApprovalSvc, err := buildBaseApprovalService(quiet, cfg.Policy, log, auditSink)
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	stateRoot := filepath.Join(workspaceRoot, ".genesis")
	workspaceRoot = workspaceRootOrDot(workspaceRoot)
	productTools, execStack, err := buildProductTools(sandboxCfg, baseApprovalSvc, log, workspaceRoot)
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	artifactControl, err := buildCLIArtifactControl(stateRoot, workspaceRoot, execStack)
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, fmt.Errorf("初始化 Artifact 控制面失败: %w", err)
	}
	environmentInjector := promptbuilder.NewEnvironmentContextInjector(cliEnvironmentContext(ctx, sandboxCfg.ExecutionProfile(), execStack.Shells))
	tools := productTools
	adapterRegistry := capservice.NewAdapterRegistry()
	marketSvc, _, err := cliskill.NewMarketplaceServiceWith(cliskill.MarketplaceOptions{
		Adapters:  adapterRegistry,
		ConfigDir: configDir,
		Install:   cfg.Skills.Install,
	})
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	capabilityRegistry := marketSvc
	capabilityTools, err := buildCapabilityTools(ctx, capabilityRegistry)
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	tools = append(tools, capabilityTools...)
	skillSvc, roots, err := buildSkillService(configDir, cfg.Skills, prof, auditSink, usageSink, capabilityRegistry, workspaceRoot)
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	startSkillWatcher(ctx, roots, skillSvc)
	catalogReq := skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI, Environment: profilemodel.EnvironmentLocal, EnabledSkills: prof.Skills.Enabled, DisabledSkills: prof.Skills.Disabled}
	inputRegistry, err := localworkspace.NewResourceRegistry(workspaceRoot)
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	inputStore, err := localworkspace.NewInputSnapshotStore(stateRoot)
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	inputStager, err := workservice.NewInputStager(inputRegistry, inputStore, idgen.NewUUIDGenerator())
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	viewProjector, err := localworkspace.NewViewProjector(inputStore)
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	viewBuilder, err := workservice.NewWorkspaceViewBuilder(inputStager, viewProjector, inputStore)
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	// 依赖预检以 Profile 可见工具集为准（含 shared builder 注册的 builtin/web），不是仅 AdditionalTools。
	enabledToolInventory := append([]string(nil), prof.Tools.Enabled...)
	skillGateway, err := skilltool.New(skilltool.Deps{Service: skillSvc, Approval: baseApprovalSvc, CatalogRequest: catalogReq, EnabledTools: enabledToolInventory})
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	explicitLoader, ok := skillGateway.(react.SkillExplicitLoader)
	if !ok {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, fmt.Errorf("Skill 网关未实现显式加载接口")
	}
	listResources, err := listskillresources.New(listskillresources.Deps{Service: skillSvc, Approval: baseApprovalSvc, CatalogRequest: catalogReq, Registry: capabilityRegistry})
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	readResource, err := readskillresource.New(readskillresource.Deps{Service: skillSvc, Approval: baseApprovalSvc, CatalogRequest: catalogReq, Registry: capabilityRegistry})
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	searchResources, err := searchskillresources.New(searchskillresources.Deps{Service: skillSvc, Approval: baseApprovalSvc, CatalogRequest: catalogReq})
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}

	skillScriptSvc, err := scriptservice.New(scriptservice.Deps{
		Skills:                skillSvc,
		Runner:                execStack.Runner,
		Approval:              baseApprovalSvc,
		SessionClient:         execStack.SessionClient,
		FileClient:            execStack.FileClient,
		WorkspaceRef:          execStack.WorkspaceRef,
		Logger:                log,
		EnablePreflight:       cfg.Skills.EnablePreflight,
		AutoRetryAfterInstall: cfg.Skills.AutoRetryAfterInstall,
		Provisioner:           localworkspace.NewProvisioner(),
		InputSnapshots:        inputStore,
		ProducedResources:     artifactControl.produced,
		RemoteSessions:        artifactControl.remoteSessions,
		Reservations:          artifactControl.reservations,
		Deliverables:          artifactControl.deliverables,
	})
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	runSkillCommand, err := runskillcommand.New(runskillcommand.Deps{
		Runner:         skillScriptSvc,
		CatalogRequest: catalogReq,
		Sandbox:        sandboxCfg.ExecutionProfile(),
		InputResolver:  inputRegistry,
		InputStager:    inputStager,
		Finalizer:      artifactControl.finalizer,
	})
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	installDeps, err := installskilldeps.New(installskilldeps.Deps{
		Skills:         skillSvc,
		Runner:         execStack.Runner,
		Approval:       baseApprovalSvc,
		CatalogRequest: catalogReq,
		Sandbox:        sandboxCfg.ExecutionProfile(),
		WorkspaceRoot:  workspaceRoot,
	})
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	installFromSource, err := installskillfromsource.New(installskillfromsource.Deps{
		Installer: marketSvc,
		Approval:  baseApprovalSvc,
		Product:   "cli",
	})
	if err != nil {
		_ = runtimeLogging.Close()
		return productRuntime{}, nil, err
	}
	tools = append(tools, skillGateway, listResources, readResource, searchResources, runSkillCommand, artifactControl.selector, installDeps, installFromSource)
	skillMatcher := &skillcollision.Matcher{Service: skillSvc, CatalogRequest: catalogReq}
	skillMentions := &react.MentionSelector{Service: skillSvc, CatalogRequest: catalogReq}
	// system 仅短硬规则；完整 catalog 挂在 Skill 工具 DescriptionFunc。
	injector := promptbuilder.ContextInjectorFunc(func(ctx context.Context, req promptbuilder.BuildRequest) (promptbuilder.Fragment, error) {
		var b strings.Builder
		b.WriteString("Skills 是任务流程包，不是可执行工具。加载技能必须调用 Skill(skill=...)；禁止把 office-ppt 等技能名当作独立工具调用。用户输入中的 $skill 或 skill:// 引用会在回合开始自动注入。请求里精确引用的已有文件由 Harness 自动绑定到当前 Run；run_skill_command 会自动 stage 已绑定输入和 command 入口脚本，禁止 run_command / Copy-Item 手动搬运。可用技能列表见 Skill 工具描述中的 <available_skills>。用户给出 Skill 的 GitHub/URL 地址要求安装时：调用 install_skill_from_source（须审批），禁止 run_command/curl/git clone 旁路。若 run_skill_command 返回 failure_kind=dependency_missing：调用 install_skill_dependencies（须审批，仅装 runtime 白名单包）后，用相同参数再跑命令（安装成功会清零重复失败计数）；sandbox_violation 勿当成缺包。收到 failure_kind=repeated_failure：禁止再次提交相同调用，必须改参或改策略。收到 failure_kind=no_progress：必须总结阻塞或询问用户，禁止继续空转。")
		b.WriteString("\n\nRun 文件落点：中间脚本/临时文件直接使用当前根下相对路径，例如 write_file(\"create_ppt.js\")。run_skill_command 返回不透明 candidate_id；required 交付物唯一匹配时 Harness 自动 Gate、发布与交付，多个匹配时只能用 select_deliverable_candidate 选择返回的 candidate_id。禁止提交物理路径或 locator，也禁止复制到仓库根或内部 runs 目录。")
		return promptbuilder.Fragment{
			Name:     "skills_instructions",
			Contents: b.String(),
		}, nil
	})
	return productRuntime{
		tools:                tools,
		promptInjectors:      []promptbuilder.ContextInjector{environmentInjector, injector},
		logger:               log,
		logging:              runtimeLogging,
		auditSink:            auditSink,
		usageSink:            usageSink,
		skillNameMatcher:     skillMatcher,
		skillMentionSelector: skillMentions,
		skillExplicitLoader:  explicitLoader,
		capabilityRegistry:   capabilityRegistry,
		adapterRegistry:      adapterRegistry,
		approvalService:      baseApprovalSvc,
		hookExecutionRunner:  execStack.Runner,
		config:               cfg,
		requestInputs:        inputRegistry,
		runInputs:            viewBuilder,
		workspaceRoot:        workspaceRoot,
		artifactRuns:         artifactControl.initializer,
		completion:           artifactControl.completion,
		qaEvidence:           artifactControl.qaEvidence,
		runResources:         []workcontract.RunResourceReleaser{skillScriptSvc},
		runtimeClosers:       []runtimeCloser{skillScriptSvc},
	}, log, nil
}

func attachMCPStack(ctx context.Context, bundle *shared.RuntimeBundle, runtime productRuntime, channel profilemodel.ChannelType, env profilemodel.RuntimeEnvironment) (*mcpstack.Stack, contract.ApprovalStore, error) {
	store, err := openCLIApprovalStore()
	if err != nil {
		return nil, nil, err
	}
	if bundle == nil || bundle.ToolGateway == nil || runtime.config == nil || !runtime.config.MCP.Enabled {
		return nil, store, nil
	}
	mcpStack, err := mcpstack.Build(ctx, mcpstack.Options{
		Config:             runtime.config,
		CapabilityIndex:    runtime.capabilityRegistry,
		ToolRegistry:       bundle.ToolGateway,
		ApprovalService:    runtime.approvalService,
		CredentialSvc:      bundle.Credentials,
		ApprovalStore:      store,
		AdapterRegistry:    runtime.adapterRegistry,
		ExistingAuthorizer: bundle.ToolGateway.Authorizer(),
		Channel:            channel,
		Environment:        env,
		TenantID:           "dev",
		Workspace:          runtime.workspaceRoot,
		FailOnRequired:     false,
		AuditSink:          runtime.auditSink,
		Tracer:             bundle.Tracer,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("初始化 MCP 栈失败: %w", err)
	}
	if mcpStack != nil && mcpStack.Authorizer != nil {
		bundle.ToolGateway.SetAuthorizer(mcpStack.Authorizer)
	}
	return mcpStack, store, nil
}

func openCLIApprovalStore() (contract.ApprovalStore, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("无法定位用户主目录以创建 mcp approvals")
	}
	path := filepath.Join(home, ".genesis-agent", "cli", "mcp-approvals.json")
	store, err := mcpstore.NewFile(path)
	if err != nil {
		return nil, fmt.Errorf("初始化 mcp approval store 失败: %w", err)
	}
	return store, nil
}

func buildCapabilityTools(ctx context.Context, registry capcontract.Registry) ([]toolcontract.Tool, error) {
	adapter := toolcapability.New(nil)
	if err := adapter.LoadFromRegistry(ctx, registry); err != nil {
		return nil, fmt.Errorf("加载tool capability失败: %w", err)
	}
	return adapter.Tools(), nil
}

func buildBaseApprovalService(quiet bool, policyCfg platformconfig.PolicyConfig, log logger.Logger, auditSink auditcontract.Sink) (approvalcontract.Service, error) {
	policyEngine, err := policyapproval.NewEngine(policyconfig.BuildEvaluator(policyCfg))
	if err != nil {
		return nil, fmt.Errorf("初始化PolicyEngine失败: %w", err)
	}
	baseApprovalSvc, err := approvalservice.New(policyEngine, newApprovalRequester(quiet), approvalmemory.NewStore(), log, approvalservice.WithAuditSink(auditSink))
	if err != nil {
		return nil, fmt.Errorf("初始化ApprovalService失败: %w", err)
	}
	return baseApprovalSvc, nil
}

// productExecStack 是 CLI 执行栈，供 run_command 与 run_skill_command 复用。
type productExecStack struct {
	Runner        execcontract.ExecutionRunner
	Shells        execcontract.ShellCapabilityProvider
	SessionClient sandboxcontract.SessionClient
	FileClient    sandboxcontract.FileSystemClient
	WorkspaceRef  sandboxcontract.WorkspaceRef
}

func buildProductTools(sandboxCfg clisandbox.Config, baseApprovalSvc approvalcontract.Service, log logger.Logger, workspaceRoot string) ([]toolcontract.Tool, productExecStack, error) {
	resolver, err := localresolver.New(workspaceRootOrDot(workspaceRoot))
	if err != nil {
		return nil, productExecStack{}, fmt.Errorf("初始化本地PathResolver失败: %w", err)
	}
	approvalSvc := fspermission.NewApprovalService(baseApprovalSvc, fspermission.NewRuntimeFilePermissions())
	locker := scheduler.NewMemoryResourceLocker()
	fileDeps := toolkit.Deps{Resolver: resolver, Backend: localfs.New(), Approval: approvalSvc, Freshness: freshness.NewMemoryTracker(), Locker: locker}
	constructors := []func(toolkit.Deps) (toolcontract.Tool, error){readfile.New, writefile.New, editfile.New, applypatchtool.New, listdir.New, walkdir.New, globtool.New, greptool.New}
	tools := make([]toolcontract.Tool, 0, len(constructors)+1)
	for _, constructor := range constructors {
		t, err := constructor(fileDeps)
		if err != nil {
			return nil, productExecStack{}, err
		}
		tools = append(tools, t)
	}
	directRunner := localexec.NewRunner()
	var shellProvider execcontract.ShellCapabilityProvider = directRunner
	if sandboxCfg.Mode == clisandbox.ModeDockerSandbox || sandboxCfg.Mode == clisandbox.ModeRemoteSandbox {
		// 远程 sandbox 的 OS/Shell 不能从宿主机推断；服务端未返回能力前仅向模型暴露 auto。
		shellProvider = nil
	}
	sandboxRunner, sessionClient, fileClient, workspaceRef, err := buildSandboxStack(directRunner, sandboxCfg, log, workspaceRoot)
	if err != nil {
		return nil, productExecStack{}, err
	}
	executionRunner, err := execservice.NewRunner(directRunner, sandboxRunner, execservice.WithLogger(log))
	if err != nil {
		return nil, productExecStack{}, err
	}
	localPTYRunner := localexec.NewLocalPTYRunner()
	sessionManager := execservice.NewSessionManager(localPTYRunner).WithApproval(baseApprovalSvc)

	runCommand, err := runcommand.New(runcommand.Deps{
		Runner:         executionRunner,
		Shells:         shellProvider,
		SessionManager: sessionManager,
		Resolver:       resolver,
		Approval:       approvalSvc,
		Locker:         locker,
		Sandbox:        sandboxCfg.ExecutionProfile(),
		BridgeTerminal: func(ctx context.Context, sessionID string) error {
			return tui.BridgeSession(ctx, sessionID, sessionManager)
		},
	})
	if err != nil {
		return nil, productExecStack{}, err
	}
	tools = append(tools, runCommand)

	writeStdin, err := writestdin.New(writestdin.Deps{
		SessionManager: sessionManager,
	})
	if err != nil {
		return nil, productExecStack{}, err
	}
	tools = append(tools, writeStdin)

	return tools, productExecStack{
		Runner:        executionRunner,
		Shells:        shellProvider,
		SessionClient: sessionClient,
		FileClient:    fileClient,
		WorkspaceRef:  workspaceRef,
	}, nil
}

func cliEnvironmentContext(ctx context.Context, sandbox execmodel.SandboxProfile, shells execcontract.ShellCapabilityProvider) promptbuilder.EnvironmentContext {
	environment := promptbuilder.EnvironmentContext{
		OS:               runtime.GOOS,
		SandboxMode:      string(sandbox.Mode),
		SandboxProvider:  sandbox.Provider,
		ExternalApproval: true,
	}
	if shells == nil {
		if sandbox.Provider == clisandbox.ProviderGenesisSandbox {
			environment.OS = ""
		}
		return environment
	}
	capabilities := shells.ShellCapabilities(ctx)
	environment.DefaultShell = string(capabilities.Default.Kind)
	environment.DefaultShellPath = capabilities.Default.Path
	for _, shell := range capabilities.Supported {
		if shell.Kind != "" {
			environment.SupportedShells = append(environment.SupportedShells, string(shell.Kind))
		}
	}
	return environment
}

func buildSandboxStack(directRunner *localexec.Runner, sandboxCfg clisandbox.Config, log logger.Logger, workspaceRoot string) (execcontract.SandboxRunner, sandboxcontract.SessionClient, sandboxcontract.FileSystemClient, sandboxcontract.WorkspaceRef, error) {
	switch sandboxCfg.Mode {
	case clisandbox.ModeDockerSandbox, clisandbox.ModeRemoteSandbox:
		client, err := sandboxhttp.New(sandboxhttp.Config{
			BaseURL: sandboxCfg.Endpoint,
			APIKey:  sandboxCfg.APIKey,
			Timeout: sandboxCfg.Timeout,
		})
		if err != nil {
			return nil, nil, nil, sandboxcontract.WorkspaceRef{}, fmt.Errorf("初始化genesis-sandbox HTTP client失败: %w", err)
		}
		workspaceRef := sandboxcontract.WorkspaceRef{
			ID:       sandboxCfg.WorkspaceID,
			Provider: clisandbox.ProviderGenesisSandbox,
		}
		runner, err := execsandbox.NewRunner(client, workspaceRef)
		if err != nil {
			return nil, nil, nil, sandboxcontract.WorkspaceRef{}, err
		}
		return runner, client, client, workspaceRef, nil
	case clisandbox.ModePlatform:
		if runtime.GOOS == "windows" {
			if !windowssandbox.IsWindowsNetworkSetupReady() {
				if windowssandbox.IsElevated() {
					// 当前已是管理员权限，直接初始化
					fmt.Printf("检测到在管理员特权终端运行且网络沙箱未就绪，自动开始初始化网络沙箱配置...\n")
					log.Info("检测到在管理员特权终端运行且网络沙箱未就绪，自动开始初始化网络沙箱配置...")
					if err := windowssandbox.RunWindowsSetupWithFlags(true); err != nil {
						fmt.Printf("自动配置网络沙箱失败: %v\n", err)
						log.Error("自动配置网络沙箱失败", "error", err)
					} else {
						log.Info("自动配置网络沙箱成功")
					}
				} else {
					// 非管理员权限：用 Win32 ShellExecuteExW 静默提权
					// 注意：如果在 go test 运行环境中，直接跳过 UAC 提权以避免测试进程挂起等待 UAC 交互。
					if flag.Lookup("test.v") != nil {
						fmt.Printf("检测到处于测试环境，跳过自动 UAC 提权。\n")
						log.Warn("检测到处于测试环境，跳过自动 UAC 提权以避免挂起。")
					} else {
						fmt.Printf("检测到网络沙箱未就绪且当前处于非管理员终端，尝试自动请求管理员权限进行网络沙箱初始化...\n")
						log.Info("检测到网络沙箱未就绪且当前处于非管理员终端，尝试自动请求管理员权限进行网络沙箱初始化...")
						cwd, _ := os.Getwd()

						// 获取真实用户的沙箱配置目录，传给提权子进程以确保写入正确位置
						localAppData := os.Getenv("LOCALAPPDATA")
						var sandboxConfigDir string
						if localAppData != "" {
							sandboxConfigDir = filepath.Join(localAppData, "genesis-agent", "sandbox")
						} else {
							home, _ := os.UserHomeDir()
							sandboxConfigDir = filepath.Join(home, ".genesis-agent", "sandbox")
						}

						// 构建提权子进程的程序路径和参数
						// 开发模式（go run）与生产模式（编译后的 exe）分别处理
						var exeFile string
						var args []string
						if strings.Contains(os.Args[0], "main") || strings.Contains(os.Args[0], "go-build") {
							exeFile = "go"
							args = []string{"run", "cmd/genesis-cli/main.go", "sandbox", "windows-setup", "--network", "--appdata", sandboxConfigDir}
						} else {
							exeFile = os.Args[0]
							args = []string{"sandbox", "windows-setup", "--network", "--appdata", sandboxConfigDir}
						}

						// 调用 Win32 ShellExecuteExW 进行 UAC 提权（SW_HIDE 静默后台运行）
						if err := windowssandbox.RunElevatedWindowsSetup(exeFile, args, cwd); err != nil {
							fmt.Printf("自动请求管理员权限失败: %v\n请手动右键管理员运行 start.bat，或运行：genesis-cli.exe sandbox windows-setup --network\n", err)
							log.Error("自动请求管理员权限失败", "error", err)
						} else {
							if windowssandbox.IsWindowsNetworkSetupReady() {
								log.Info("通过管理员提权自动配置网络沙箱成功")
								fmt.Printf("自动配置网络沙箱成功！\n")
								windowssandbox.ClearElevationResult(cwd)
							} else {
								result, _ := windowssandbox.ReadElevationResult(cwd)
								errStr := "未知错误，子进程未写入任何异常信息。"
								if result != nil && result.Error != "" {
									errStr = result.Error
								}
								log.Warn("管理员提权已结束，但网络沙箱仍未就绪", "error", errStr)
								fmt.Printf("管理员提权已结束，但网络沙箱仍未就绪。\n错误输出:\n%s\n", errStr)
							}
						}
					}
				}
			}
		}
		runner, err := localexec.NewSandboxRunner(directRunner, localexec.SandboxRunnerOptions{})
		if err != nil {
			return nil, nil, nil, sandboxcontract.WorkspaceRef{}, err
		}
		return runner, nil, nil, sandboxcontract.WorkspaceRef{}, nil
	default:
		return nil, nil, nil, sandboxcontract.WorkspaceRef{}, nil
	}
}

func buildSkillService(configDir string, cfg platformconfig.SkillsConfig, prof profilemodel.Profile, auditSink auditcontract.Sink, usageSink usagecontract.Sink, visibility capcontract.Registry, workspaceRoot string) (skillcontract.Service, []localskill.Root, error) {
	roots := defaultSkillRoots(configDir, workspaceRootOrDot(workspaceRoot))
	installedRoots, err := cliskill.InstalledSkillRoots(context.Background())
	if err != nil {
		return nil, nil, err
	}
	roots = append(roots, installedRoots...)
	prof.Skills.Enabled = appendUniqueStrings(prof.Skills.Enabled, cfg.Enabled...)
	prof.Skills.Disabled = appendUniqueStrings(prof.Skills.Disabled, cfg.Disabled...)

	for _, source := range cfg.Sources {
		if source.Path == "" {
			continue
		}
		scope := skillmodel.Scope(source.Scope)
		if scope == "" {
			scope = skillmodel.ScopeUser
		}
		roots = append(roots, localskill.Root{Path: source.Path, Scope: scope})
	}
	for _, source := range prof.Skills.Sources {
		if source.Path == "" {
			continue
		}
		scope := skillmodel.Scope(source.Scope)
		if scope == "" {
			scope = skillmodel.ScopeProject
		}
		roots = append(roots, localskill.Root{Path: source.Path, Scope: scope})
	}
	roots = dedupeRoots(roots)
	source, err := localskill.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindHost, ID: "cli-local"}, roots, skillparser.New())
	if err != nil {
		return nil, nil, err
	}
	systemFS, err := skillembedded.SystemFS()
	if err != nil {
		return nil, nil, fmt.Errorf("初始化CLI内置Skills失败: %w", err)
	}
	systemSource, err := skillembedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "cli-system"}, skillmodel.ScopeSystem, systemFS, skillparser.New())
	if err != nil {
		return nil, nil, fmt.Errorf("初始化CLI内置Skills失败: %w", err)
	}
	return skillservice.New([]skillcontract.Source{systemSource, source}, skillservice.Options{AuditSink: auditSink, UsageSink: usageSink, Visibility: visibility}), roots, nil
}

func startSkillWatcher(ctx context.Context, roots []localskill.Root, svc skillcontract.Service) {
	if svc == nil || len(roots) == 0 {
		return
	}
	watchRoots := make([]skillcontract.WatchRoot, 0, len(roots))
	for _, root := range roots {
		watchRoots = append(watchRoots, skillcontract.WatchRoot{Path: root.Path, Scope: root.Scope, Recursive: true})
	}
	changes, err := localskill.NewWatcher(localskill.WatcherOptions{}).Watch(ctx, watchRoots)
	if err != nil {
		return
	}
	go func() {
		for range changes {
			svc.ClearCache()
		}
	}()
}

func defaultSkillRoots(configDir string, workspaceRoot string) []localskill.Root {
	roots := make([]localskill.Root, 0, 3)
	if workspaceRoot = strings.TrimSpace(workspaceRoot); workspaceRoot != "" {
		roots = append(roots, localskill.Root{Path: filepath.Join(workspaceRoot, ".genesis", "skills"), Scope: skillmodel.ScopeProject})
	}
	if configDir != "" {
		roots = append(roots, localskill.Root{Path: filepath.Join(configDir, "skills"), Scope: skillmodel.ScopeUser})
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, localskill.Root{Path: filepath.Join(home, ".genesis-agent", "cli", "skills"), Scope: skillmodel.ScopeUser})
		roots = append(roots, localskill.Root{Path: filepath.Join(home, ".genesis", "skills"), Scope: skillmodel.ScopeUser})
	}
	return dedupeRoots(roots)
}

func workspaceRootOrDot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		if wd, err := os.Getwd(); err == nil && strings.TrimSpace(wd) != "" {
			root = wd
		}
	}
	if root == "" {
		return "."
	}
	if abs, err := filepath.Abs(root); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(root)
}

func appendUniqueStrings(base []string, extra ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, value := range append(append([]string{}, base...), extra...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
func dedupeRoots(roots []localskill.Root) []localskill.Root {
	out := make([]localskill.Root, 0, len(roots))
	seen := map[string]struct{}{}
	for _, root := range roots {
		path := filepath.Clean(root.Path)
		key := path + "|" + string(root.Scope)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, localskill.Root{Path: path, Scope: root.Scope})
	}
	return out
}

func newApprovalRequester(quiet bool) approvalcontract.Requester {
	if quiet {
		return cliapproval.GlobalTUIRequester
	}
	return cliapproval.NewTerminalRequester(os.Stdin, os.Stderr)
}

type cliArtifactControl struct {
	produced       workcontract.ProducedResourceRegistrar
	remoteSessions scriptservice.RemoteSessionBinder
	reservations   artifactcontract.OutputReservationAllocator
	deliverables   artifactcontract.DeliverableSpecStore
	finalizer      artifactcontract.RequiredDeliverableFinalizer
	initializer    artifactcontract.RunInitializer
	completion     artifactcontract.CompletionPolicy
	qaEvidence     artifactcontract.QAEvidenceRecorder
	selector       toolcontract.Tool
}

func buildCLIArtifactControl(stateRoot, workspaceRoot string, execStack productExecStack) (cliArtifactControl, error) {
	var downloader workspaceadapter.ArtifactByteDownloader
	if execStack.FileClient != nil {
		if d, ok := execStack.FileClient.(workspaceadapter.ArtifactByteDownloader); ok {
			downloader = d
		}
	}
	built, err := localartifactcontrol.Build(localartifactcontrol.Options{
		StateRoot:             stateRoot,
		DeliveryWorkspaceRoot: workspaceRoot,
		FileClient:            execStack.FileClient,
		ArtifactDownloader:    downloader,
	})
	if err != nil {
		return cliArtifactControl{}, err
	}
	return cliArtifactControl{
		produced:       built.Produced,
		remoteSessions: built.RemoteSessions,
		reservations:   built.Reservations,
		deliverables:   built.Deliverables,
		finalizer:      built.Finalizer,
		initializer:    built.Initializer,
		completion:     built.Completion,
		qaEvidence:     built.QAEvidence,
		selector:       built.Selector,
	}, nil
}

func buildLocalRunWorkspace(projectDir string, requestInputs workcontract.RequestInputPlanner, runInputs workcontract.RunInputBinder, artifactRuns artifactcontract.RunInitializer, completion artifactcontract.CompletionPolicy, qaEvidence artifactcontract.QAEvidenceRecorder, runResources []workcontract.RunResourceReleaser) (app.RunWorkspaceRuntime, error) {
	stateRoot := filepath.Join(projectDir, ".genesis")
	manifests, err := localworkspace.NewManifestStore(stateRoot)
	if err != nil {
		return app.RunWorkspaceRuntime{}, err
	}
	ids := idgen.NewUUIDGenerator()
	resolver, err := workservice.NewWorkspaceResolver(ids)
	if err != nil {
		return app.RunWorkspaceRuntime{}, err
	}
	localProvisioner := localworkspace.NewProvisioner()
	provisioner, err := workservice.NewBackendProvisioner(localProvisioner, localProvisioner, workspaceadapter.NewProvisioner())
	if err != nil {
		return app.RunWorkspaceRuntime{}, err
	}
	preparer, err := workservice.NewRunPreparer(workservice.RunPreparerDeps{IDs: ids, Resolver: resolver, StateRoots: localworkspace.StateRootResolver{ProjectStateDir: stateRoot, UserStateDir: stateRoot}, Provisioner: provisioner, Manifests: manifests, Inputs: runInputs})
	if err != nil {
		return app.RunWorkspaceRuntime{}, err
	}
	modes := []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject, execmodel.WorkspaceModeTask, execmodel.WorkspaceModeSession}
	profile := agentappmodel.EffectiveProfile{ID: "code", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeProject, AllowedModes: modes, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	appResolver, err := agentappmemory.NewResolver(profile.ID, []agentappmodel.EffectiveProfile{profile})
	if err != nil {
		return app.RunWorkspaceRuntime{}, err
	}
	projectRef := &workmodel.ResourceRef{Authority: "host", Scheme: "project", ID: filepath.ToSlash(projectDir), Path: "."}
	return app.RunWorkspaceRuntime{Preparer: preparer, AgentApps: appResolver, IntentResolver: workservice.NewTaskIntentResolver(), RequestInputs: requestInputs, ProjectRoot: projectRef, ProjectDir: projectDir, ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite, ArtifactRuns: artifactRuns, Completion: completion, QAEvidence: qaEvidence, WorkspaceCompletion: localworkspace.NewGitChangeGuard(), RunResources: append([]workcontract.RunResourceReleaser(nil), runResources...)}, nil
}

func buildWebOptions(cfg *platformconfig.Config, log logger.Logger) (shared.WebBuildOptions, error) {
	cache := webcache.NewCache()
	policy := webservice.NewPolicy(nil, nil)

	braveKey := cfg.Web.BraveAPIKey
	if braveKey == "" {
		braveKey = os.Getenv("BRAVE_SEARCH_API_KEY")
	}

	searxngURL := cfg.Web.SearXNGBaseURL
	if searxngURL == "" {
		searxngURL = os.Getenv("SEARXNG_BASE_URL")
	}

	tavilyKey := cfg.Web.TavilyAPIKey
	if tavilyKey == "" {
		tavilyKey = os.Getenv("TAVILY_API_KEY")
	}

	exaKey := cfg.Web.ExaAPIKey
	if exaKey == "" {
		exaKey = os.Getenv("EXA_API_KEY")
	}

	serpapiKey := cfg.Web.SerpAPIKey
	if serpapiKey == "" {
		serpapiKey = os.Getenv("SERPAPI_API_KEY")
	}

	var providers []webcontract.SearchProvider
	providers = append(providers, ddg.NewProvider(""))
	providers = append(providers, brave.NewProvider(braveKey, ""))
	providers = append(providers, searxng.NewProvider(searxngURL))
	providers = append(providers, tavily.NewProvider(tavilyKey, ""))
	providers = append(providers, exa.NewProvider(exaKey, ""))
	providers = append(providers, serpapi.NewProvider(serpapiKey, ""))

	defaultSearcher := "duckduckgo"
	if serpapiKey != "" {
		defaultSearcher = "serpapi"
	}
	if exaKey != "" {
		defaultSearcher = "exa"
	}
	if tavilyKey != "" {
		defaultSearcher = "tavily"
	}
	if searxngURL != "" {
		defaultSearcher = "searxng"
	}
	if braveKey != "" {
		defaultSearcher = "brave"
	}

	searchService := webservice.NewSearchService(providers, defaultSearcher, policy, cache, log)
	httpFetcher := httpfetcher.NewFetcher()
	htmlExtractor := webextractor.NewExtractor()
	fetchService := webservice.NewFetchService(httpFetcher, htmlExtractor, policy, cache)

	return shared.WebBuildOptions{
		Enabled:        true,
		Searcher:       searchService,
		Fetcher:        fetchService,
		RegisterSearch: true,
		RegisterFetch:  true,
	}, nil
}
