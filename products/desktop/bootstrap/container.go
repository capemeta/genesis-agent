package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"genesis-agent/internal/app"
	shared "genesis-agent/internal/bootstrap"
	agentappmemory "genesis-agent/internal/capabilities/agentapp/adapter/memory"
	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	approvalmemory "genesis-agent/internal/capabilities/approval/adapter/memory"
	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	approvalservice "genesis-agent/internal/capabilities/approval/service"
	auditmemory "genesis-agent/internal/capabilities/audit/adapter/memory"
	capcontract "genesis-agent/internal/capabilities/capability/contract"
	capservice "genesis-agent/internal/capabilities/capability/service"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	execservice "genesis-agent/internal/capabilities/execution/service"
	"genesis-agent/internal/capabilities/filesystem/freshness"
	fspermission "genesis-agent/internal/capabilities/filesystem/permission"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	viewimage "genesis-agent/internal/capabilities/media/tool/view_image"
	mcpstore "genesis-agent/internal/capabilities/mcp/adapter/store"
	"genesis-agent/internal/capabilities/mcp/contract"
	mcpstack "genesis-agent/internal/capabilities/mcp/stack"
	policyapproval "genesis-agent/internal/capabilities/policy/adapter/approval"
	policyconfig "genesis-agent/internal/capabilities/policy/adapter/config"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
	toolvalidation "genesis-agent/internal/capabilities/tool/validation"
	workservice "genesis-agent/internal/capabilities/workspace/service"
	platformconfig "genesis-agent/internal/platform/config"
	"genesis-agent/internal/platform/idgen"
	"genesis-agent/internal/platform/logger"
	multicontract "genesis-agent/internal/runtime/multiagent/contract"
	multiagentmodel "genesis-agent/internal/runtime/multiagent/model"
	multiprojection "genesis-agent/internal/runtime/multiagent/projection"
	promptbuilder "genesis-agent/internal/runtime/prompt"
	desktopprofile "genesis-agent/products/desktop/internal/profile"
	localartifactcontrol "genesis-agent/shared/local/artifactcontrol"
	localcollab "genesis-agent/shared/local/collab"
	localexec "genesis-agent/shared/local/execution"
	localfs "genesis-agent/shared/local/filesystem"
	localresolver "genesis-agent/shared/local/pathresolver"
	"genesis-agent/shared/local/skillmarket"
	localworkspace "genesis-agent/shared/local/workspace"
)

// Container 装配 Desktop 产品运行时（复用内核 + MCP；Wails UI 另开）。
type Container struct {
	configDir string
	quiet     bool

	once     sync.Once
	initErr  error
	bundle   *shared.RuntimeBundle
	logging  *logger.RuntimeLogging
	mcpStack *mcpstack.Stack
	mcpStore contract.ApprovalStore
}

// NewContainer 创建 Desktop 容器。
func NewContainer(configDir string, quiet bool) *Container {
	if strings.TrimSpace(configDir) == "" {
		configDir = "configs"
	}
	return &Container{configDir: configDir, quiet: quiet}
}

// Init 初始化 Agent 内核与 MCP 栈。
func (c *Container) Init(ctx context.Context) error {
	c.once.Do(func() {
		cfg, err := platformconfig.LoadWithOptions(c.configDir, platformconfig.LoadOptions{
			Product: "desktop",
		})
		if err != nil {
			c.initErr = fmt.Errorf("加载 Desktop 配置失败: %w", err)
			return
		}
		runtimeLogging, err := logger.NewRuntimeLogging(cfg.Log, logger.RuntimeLoggingOptions{
			ConfigDir: c.configDir,
			Quiet:     c.quiet,
		})
		if err != nil {
			c.initErr = fmt.Errorf("初始化运行日志失败: %w", err)
			return
		}
		c.logging = runtimeLogging
		auditSink := auditmemory.NewSink()
		approvalSvc, err := buildDesktopApproval(cfg.Policy, runtimeLogging.AgentLogger)
		if err != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			c.initErr = err
			return
		}
		prof := desktopprofile.DefaultProfile(cfg.MCP.Enabled)
		var capabilityRegistry capcontract.Registry
		var adapterReg *capservice.AdapterRegistry
		if cfg.MCP.Enabled {
			adapterReg = capservice.NewAdapterRegistry()
			capabilityRegistry, err = loadDesktopCapabilityIndex(adapterReg)
			if err != nil {
				_ = runtimeLogging.Close()
				c.logging = nil
				c.initErr = fmt.Errorf("初始化 Desktop capability index失败: %w", err)
				return
			}
		}
		hostRunner := localexec.NewRunner()
		hookRunner, err := execservice.NewRunner(hostRunner, nil, execservice.WithLogger(runtimeLogging.AgentLogger))
		if err != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			c.initErr = fmt.Errorf("初始化 Desktop Hook 执行器失败: %w", err)
			return
		}

		shellCapabilities := hostRunner.ShellCapabilities(ctx)
		supportedShells := make([]string, 0, len(shellCapabilities.Supported))
		for _, shell := range shellCapabilities.Supported {
			if shell.Kind != "" {
				supportedShells = append(supportedShells, string(shell.Kind))
			}
		}
		environmentInjector := promptbuilder.NewEnvironmentContextInjector(promptbuilder.EnvironmentContext{
			OS:               runtime.GOOS,
			DefaultShell:     string(shellCapabilities.Default.Kind),
			DefaultShellPath: shellCapabilities.Default.Path,
			SupportedShells:  supportedShells,
			SandboxMode:      "disabled",
			ExternalApproval: true,
		})

		runWorkspace, artifactControl, err := buildDesktopRunWorkspace()
		if err != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			c.initErr = fmt.Errorf("初始化 Desktop Run 工作空间失败: %w", err)
			return
		}
		workspaceRoot := "."
		if wd, err := os.Getwd(); err == nil && strings.TrimSpace(wd) != "" {
			workspaceRoot = wd
		}
		collabStoreDir := filepath.Join(workspaceRoot, ".genesis", "runtime", "collab")
		collabStore, err := localcollab.NewFileStore(collabStoreDir)
		if err != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			c.initErr = fmt.Errorf("初始化 Desktop 协作模式存储失败: %w", err)
			return
		}
		var extraTools []toolcontract.Tool
		if viewTool, viewErr := buildDesktopViewImage(workspaceRoot, approvalSvc, artifactControl); viewErr != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			c.initErr = fmt.Errorf("初始化 view_image 失败: %w", viewErr)
			return
		} else if viewTool != nil {
			extraTools = append(extraTools, viewTool)
		}
		c.bundle, c.initErr = shared.BuildAgentService(ctx, shared.BuildOptions{
			Product:                        "desktop",
			ConfigDir:                      c.configDir,
			Quiet:                          c.quiet,
			RouteName:                      "chat",
			DefaultAgentID:                 "desktop-default-agent",
			DefaultAgentName:               "Genesis Desktop Agent",
			Profile:                        prof,
			RunWorkspace:                   runWorkspace,
			AdditionalTools:                extraTools,
			PromptInjectors:                []promptbuilder.ContextInjector{environmentInjector},
			Logger:                         runtimeLogging.AgentLogger,
			AuditSink:                      auditSink,
			CollabStore:                    collabStore,
			PlanDocuments:                  localcollab.NewFilePlanDocuments(workspaceRoot),
			WorkspaceRoot:                  workspaceRoot,
			SubAgentMaxConcurrent:          4,
			SubAgentDelegationPosture:      "proactive",
			SubAgentProjection:             multiprojection.NewMemorySink(multiagentmodel.ProjectionChannelDesktop),
			SubAgentIncludeUserDefinitions: true,
			SubAgentCapabilityRegistry:     capabilityRegistry,
			SubAgentApproval:               approvalSvc,
			HookExecutionRunner:            hookRunner,
			HookApproval:                   approvalSvc,
		})
		if c.initErr != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			return
		}
		if !cfg.MCP.Enabled || c.bundle == nil || c.bundle.ToolGateway == nil {
			return
		}
		store, err := openDesktopApprovalStore()
		if err != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			c.bundle = nil
			c.initErr = err
			return
		}
		workspace := "."
		if wd, err := os.Getwd(); err == nil && strings.TrimSpace(wd) != "" {
			workspace = wd
		}
		mcpStack, err := mcpstack.Build(ctx, mcpstack.Options{
			Config:             cfg,
			CapabilityIndex:    capabilityRegistry,
			ToolRegistry:       c.bundle.ToolGateway,
			ApprovalService:    approvalSvc,
			CredentialSvc:      c.bundle.Credentials,
			ApprovalStore:      store,
			AdapterRegistry:    adapterReg,
			ExistingAuthorizer: c.bundle.ToolGateway.Authorizer(),
			Channel:            profilemodel.ChannelDesktop,
			Environment:        profilemodel.EnvironmentLocal,
			TenantID:           "dev",
			Workspace:          workspace,
			FailOnRequired:     false,
			AuditSink:          auditSink,
			Tracer:             c.bundle.Tracer,
		})
		if err != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			c.bundle = nil
			c.initErr = fmt.Errorf("初始化 Desktop MCP 栈失败: %w", err)
			return
		}
		if mcpStack != nil && mcpStack.Authorizer != nil {
			c.bundle.ToolGateway.SetAuthorizer(mcpStack.Authorizer)
		}
		c.mcpStack = mcpStack
		c.mcpStore = store
		if err := toolvalidation.ValidateEnabled(c.bundle.ToolRegistry, prof.Tools.Enabled); err != nil {
			if c.mcpStack != nil {
				_ = c.mcpStack.Close(context.Background())
				c.mcpStack = nil
			}
			_ = runtimeLogging.Close()
			c.logging = nil
			c.bundle = nil
			c.initErr = fmt.Errorf("Desktop 工具装配与 Profile 不一致: %w", err)
			return
		}
	})
	return c.initErr
}

// Service 返回 AgentService。
func (c *Container) Service() app.AgentService {
	if c == nil || c.bundle == nil {
		return nil
	}
	return c.bundle.AgentService
}

// MCPStack 返回 MCP 栈。
func (c *Container) MCPStack() *mcpstack.Stack {
	if c == nil {
		return nil
	}
	return c.mcpStack
}

// SubAgentProjectionReader 为未来 Wails ViewModel 提供安全的控制面事件查询。
func (c *Container) SubAgentProjectionReader() multicontract.ProjectionReader {
	if c == nil || c.bundle == nil {
		return nil
	}
	reader, _ := c.bundle.SubAgentProjection.(multicontract.ProjectionReader)
	return reader
}

// Close 释放资源。
func (c *Container) Close() error {
	if c == nil {
		return nil
	}
	var first error
	if c.mcpStack != nil {
		if err := c.mcpStack.Close(context.Background()); err != nil {
			first = err
		}
		c.mcpStack = nil
	}
	if c.logging != nil {
		if err := c.logging.Close(); err != nil && first == nil {
			first = err
		}
		c.logging = nil
	}
	return first
}

func buildDesktopApproval(policyCfg platformconfig.PolicyConfig, log logger.Logger) (approvalcontract.Service, error) {
	engine, err := policyapproval.NewEngine(policyconfig.BuildEvaluator(policyCfg))
	if err != nil {
		return nil, err
	}
	return approvalservice.New(engine, desktopAskApprover{}, approvalmemory.NewStore(), log)
}

type desktopAskApprover struct{}

func (desktopAskApprover) RequestApproval(ctx context.Context, req approvalmodel.Request, result approvalmodel.PolicyResult) (approvalmodel.Decision, error) {
	if err := ctx.Err(); err != nil {
		return approvalmodel.Decision{}, err
	}
	// 退出规划必须经用户批准；UI 未接入前不得 headless 自动放行。
	if req.Action == approvalmodel.ActionPlanExitApprove {
		return approvalmodel.Decision{
			Type:   approvalmodel.DecisionDenied,
			Reason: "退出规划模式需要用户批准（Desktop 审批 UI 待接入）",
		}, nil
	}
	switch result.Type {
	case approvalmodel.PolicyDeny:
		return approvalmodel.Decision{Type: approvalmodel.DecisionDenied, Reason: result.Reason}, nil
	case approvalmodel.PolicyAllow:
		return approvalmodel.Decision{Type: approvalmodel.DecisionApproved, Reason: result.Reason}, nil
	default:
		return approvalmodel.Decision{
			Type:   approvalmodel.DecisionApprovedForScope,
			Scope:  approvalmodel.GrantScopeSession,
			Reason: "desktop headless auto-approve ask（Wails 弹窗待接入）",
		}, nil
	}
}

func buildDesktopRunWorkspace() (app.RunWorkspaceRuntime, localartifactcontrol.Control, error) {
	stateRoot, inbox, err := desktopStorageRoots()
	if err != nil {
		return app.RunWorkspaceRuntime{}, localartifactcontrol.Control{}, err
	}
	artifactControl, err := localartifactcontrol.Build(localartifactcontrol.Options{
		StateRoot:             stateRoot,
		DeliveryWorkspaceRoot: inbox,
	})
	if err != nil {
		return app.RunWorkspaceRuntime{}, localartifactcontrol.Control{}, fmt.Errorf("装配 Desktop Artifact 控制面失败: %w", err)
	}
	manifests, err := localworkspace.NewManifestStore(stateRoot)
	if err != nil {
		return app.RunWorkspaceRuntime{}, localartifactcontrol.Control{}, err
	}
	ids := idgen.NewUUIDGenerator()
	resolver, err := workservice.NewWorkspaceResolver(ids)
	if err != nil {
		return app.RunWorkspaceRuntime{}, localartifactcontrol.Control{}, err
	}
	preparer, err := workservice.NewRunPreparer(workservice.RunPreparerDeps{IDs: ids, Resolver: resolver, StateRoots: localworkspace.StateRootResolver{UserStateDir: stateRoot}, Provisioner: localworkspace.NewProvisioner(), Manifests: manifests})
	if err != nil {
		return app.RunWorkspaceRuntime{}, localartifactcontrol.Control{}, err
	}
	modes := []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask, execmodel.WorkspaceModeSession}
	appProfile := agentappmodel.EffectiveProfile{ID: "code", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: modes, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	appResolver, err := agentappmemory.NewResolver(appProfile.ID, []agentappmodel.EffectiveProfile{appProfile})
	if err != nil {
		return app.RunWorkspaceRuntime{}, localartifactcontrol.Control{}, err
	}
	return app.RunWorkspaceRuntime{
		Preparer: preparer, AgentApps: appResolver, IntentResolver: workservice.NewTaskIntentResolver(),
		ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite,
		ArtifactRuns: artifactControl.Initializer, Completion: artifactControl.Completion, QAEvidence: artifactControl.QAEvidence,
	}, artifactControl, nil
}

func buildDesktopViewImage(workspaceRoot string, baseApproval approvalcontract.Service, control localartifactcontrol.Control) (toolcontract.Tool, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		workspaceRoot = "."
	}
	resolver, err := localresolver.New(workspaceRoot)
	if err != nil {
		return nil, err
	}
	filePerms, err := fspermission.NewRuntimeFilePermissionsWithProjectStore(context.Background(), workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("初始化文件运行时授权失败: %w", err)
	}
	approvalSvc := fspermission.NewApprovalService(baseApproval, filePerms)
	deps := toolkit.Deps{
		Resolver: resolver, Backend: localfs.New(), Approval: approvalSvc,
		Freshness: freshness.NewMemoryTracker(), Locker: scheduler.NewMemoryResourceLocker(),
	}
	t, err := viewimage.New(deps)
	if err != nil {
		return nil, err
	}
	if control.ProducedStore != nil {
		t = viewimage.WithProducedStore(t, control.ProducedStore)
	}
	if control.Readers != nil {
		t = viewimage.WithReaders(t, control.Readers)
	}
	if control.Manifests != nil {
		t = viewimage.WithManifests(t, control.Manifests)
	}
	return t, nil
}

func desktopStorageRoots() (stateRoot, inbox string, err error) {
	dataRoot, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(dataRoot, "genesis-agent", "desktop", "state"), filepath.Join(home, "Genesis Agent", "Inbox"), nil
}

func loadDesktopCapabilityIndex(adapters capcontract.RuntimeAdapterRegistry) (capcontract.Registry, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("无法定位用户主目录")
	}
	indexFile := filepath.Join(home, ".genesis-agent", "desktop", "capability-index.json")
	return capservice.NewRegistry(capservice.Options{
		Store:    skillmarket.NewCapabilityIndexStore(indexFile),
		Adapters: adapters,
	})
}

func openDesktopApprovalStore() (contract.ApprovalStore, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("无法定位用户主目录以创建 mcp approvals")
	}
	path := filepath.Join(home, ".genesis-agent", "desktop", "mcp-approvals.json")
	return mcpstore.NewFile(path)
}
