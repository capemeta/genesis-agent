package bootstrap

import (
	"context"
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
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	approvalservice "genesis-agent/internal/capabilities/approval/service"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	auditfile "genesis-agent/internal/capabilities/audit/adapter/file"
	auditmemory "genesis-agent/internal/capabilities/audit/adapter/memory"
	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	capfile "genesis-agent/internal/capabilities/capability/adapter/file"
	capcontract "genesis-agent/internal/capabilities/capability/contract"
	capservice "genesis-agent/internal/capabilities/capability/service"
	execsandbox "genesis-agent/internal/capabilities/execution/adapter/sandbox"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	execservice "genesis-agent/internal/capabilities/execution/service"
	mcpstore "genesis-agent/internal/capabilities/mcp/adapter/store"
	"genesis-agent/internal/capabilities/mcp/contract"
	mcpstack "genesis-agent/internal/capabilities/mcp/stack"
	policyapproval "genesis-agent/internal/capabilities/policy/adapter/approval"
	policyconfig "genesis-agent/internal/capabilities/policy/adapter/config"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	sandboxhttp "genesis-agent/internal/capabilities/sandbox/adapter/http"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	scriptservice "genesis-agent/internal/capabilities/skill/script/service"
	toolvalidation "genesis-agent/internal/capabilities/tool/validation"
	usagefile "genesis-agent/internal/capabilities/usage/adapter/file"
	usagememory "genesis-agent/internal/capabilities/usage/adapter/memory"
	usagecontract "genesis-agent/internal/capabilities/usage/contract"
	workspaceadapter "genesis-agent/internal/capabilities/workspace/adapter/sandbox"
	workspacecontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	workservice "genesis-agent/internal/capabilities/workspace/service"
	platformconfig "genesis-agent/internal/platform/config"
	"genesis-agent/internal/platform/idgen"
	"genesis-agent/internal/platform/logger"
	multicontract "genesis-agent/internal/runtime/multiagent/contract"
	multiagentmodel "genesis-agent/internal/runtime/multiagent/model"
	multiprojection "genesis-agent/internal/runtime/multiagent/projection"
	promptbuilder "genesis-agent/internal/runtime/prompt"
	enterprisemcp "genesis-agent/products/enterprise/internal/mcp"
	"genesis-agent/products/enterprise/internal/profile"
	"genesis-agent/shared/skillstack"
)

func loadEnterpriseCapabilityIndex(adapters capcontract.RuntimeAdapterRegistry) (capcontract.Registry, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("无法定位用户主目录")
	}
	indexFile := filepath.Join(home, ".genesis-agent", "enterprise", "capability-index.json")
	return capservice.NewRegistry(capservice.Options{
		Store:    capfile.New(indexFile),
		Adapters: adapters,
	})
}

func openEnterpriseApprovalStore() (contract.ApprovalStore, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("无法定位用户主目录以创建 mcp approvals")
	}
	// Phase 2.5 前用文件存储；DB/OAuth 不在本轮范围。
	path := filepath.Join(home, ".genesis-agent", "enterprise", "mcp-approvals.json")
	store, err := mcpstore.NewFile(path)
	if err != nil {
		return nil, err
	}
	return store, nil
}

// Container 是 Enterprise 产品的装配容器。
type Container struct {
	configDirRef *string
	quiet        bool

	once       sync.Once
	initErr    error
	bundle     *shared.RuntimeBundle
	logging    *logger.RuntimeLogging
	mcpStack   *mcpstack.Stack
	skillStack *skillstack.Stack
	deps       Dependencies
}

// Dependencies 是 Enterprise 必须由部署层注入的租户级持久化依赖。
// 这些依赖不得回退为实例本地文件或进程内存。
type Dependencies struct {
	RunManifests      workspacecontract.RunManifestStore
	ProducedResources workspacecontract.ProducedResourceRegistrar
	RemoteSessions    scriptservice.RemoteSessionBinder
	Reservations      artifactcontract.OutputReservationAllocator
	Deliverables      artifactcontract.DeliverableSpecStore
	ArtifactRuns      artifactcontract.RunInitializer
	Finalizer         artifactcontract.RequiredDeliverableFinalizer
	Completion        artifactcontract.CompletionPolicy
	QAEvidence        artifactcontract.QAEvidenceRecorder
}

// ContainerOptions 描述 Enterprise 容器装配参数。
type ContainerOptions struct {
	ConfigDirRef *string
	Quiet        bool
	Dependencies Dependencies
}

// NewContainer 创建 Enterprise 产品容器。
func NewContainer(opts ContainerOptions) *Container {
	return &Container{configDirRef: opts.ConfigDirRef, quiet: opts.Quiet, deps: opts.Dependencies}
}

// Init 初始化 Enterprise 产品运行时依赖。
func (c *Container) Init(ctx context.Context) error {
	c.once.Do(func() {
		if c.deps.RunManifests == nil {
			c.initErr = fmt.Errorf("Enterprise 租户 RunManifestStore 未配置；禁止回退到进程内存或实例本地文件")
			return
		}
		if c.deps.ProducedResources == nil || c.deps.RemoteSessions == nil || c.deps.Reservations == nil || c.deps.Deliverables == nil || c.deps.ArtifactRuns == nil || c.deps.Finalizer == nil || c.deps.Completion == nil || c.deps.QAEvidence == nil {
			c.initErr = fmt.Errorf("Enterprise 租户 ProducedResource/Artifact 控制面未完整配置；禁止回退到实例本地文件或旧发布链")
			return
		}
		configDir := ""
		if c.configDirRef != nil {
			configDir = *c.configDirRef
		}
		cfg, err := platformconfig.LoadWithOptions(configDir, platformconfig.LoadOptions{Product: "enterprise", EnsureUserConfig: true})
		if err != nil {
			c.initErr = fmt.Errorf("加载Enterprise产品配置失败: %w", err)
			return
		}
		runtimeLogging, err := logger.NewRuntimeLogging(cfg.Log, logger.RuntimeLoggingOptions{
			ConfigDir: configDir,
			Quiet:     c.quiet,
		})
		if err != nil {
			c.initErr = fmt.Errorf("初始化运行日志失败: %w", err)
			return
		}
		c.logging = runtimeLogging
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

		prof := profile.DefaultProfile(cfg.MCP.Enabled)
		var capabilityRegistry capcontract.Registry
		var adapterReg *capservice.AdapterRegistry
		if cfg.MCP.Enabled {
			adapterReg = capservice.NewAdapterRegistry()
			capabilityRegistry, err = loadEnterpriseCapabilityIndex(adapterReg)
			if err != nil {
				_ = runtimeLogging.Close()
				c.logging = nil
				c.initErr = fmt.Errorf("初始化Enterprise capability index失败: %w", err)
				return
			}
		}
		approvalSvc, err := buildEnterpriseApproval(cfg.Policy, runtimeLogging.AgentLogger, auditSink)
		if err != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			c.initErr = err
			return
		}
		execStack, err := buildEnterpriseExecStack(cfg.Sandbox, runtimeLogging.AgentLogger)
		if err != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			c.initErr = fmt.Errorf("初始化Enterprise执行栈失败: %w", err)
			return
		}
		skillStack, err := skillstack.BuildEmbedded(skillstack.Options{
			Product:               profilemodel.ChannelEnterprise,
			Environment:           profilemodel.EnvironmentServer,
			Approval:              approvalSvc,
			Logger:                runtimeLogging.AgentLogger,
			EnabledTools:          append([]string{}, prof.Tools.Enabled...),
			EnablePreflight:       cfg.Skills.EnablePreflight,
			AutoRetryAfterInstall: cfg.Skills.AutoRetryAfterInstall,
			StateRoot:             workmodel.StateRoot{ID: "enterprise-workspace:" + execStack.WorkspaceRef.ID, Authority: "executor"},
			Provisioner:           workspaceadapter.NewProvisioner(),
			ProducedResources:     c.deps.ProducedResources,
			RemoteSessions:        c.deps.RemoteSessions,
			Reservations:          c.deps.Reservations,
			Deliverables:          c.deps.Deliverables,
			Finalizer:             c.deps.Finalizer,
			Exec:                  execStack,
		})
		if err != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			c.initErr = fmt.Errorf("初始化Enterprise Skill栈失败: %w", err)
			return
		}
		c.skillStack = skillStack
		injectors := make([]promptbuilder.ContextInjector, 0, 2)
		injectors = append(injectors, promptbuilder.NewEnvironmentContextInjector(enterpriseEnvironmentContext(ctx, execStack)))
		if skillStack.PromptInjector != nil {
			injectors = append(injectors, skillStack.PromptInjector)
		}
		runWorkspace, err := buildEnterpriseRunWorkspace(execStack.WorkspaceRef.ID, c.deps.RunManifests, c.deps.ArtifactRuns, c.deps.Completion, c.deps.QAEvidence)
		if err != nil {
			c.closeSkillStack()
			_ = runtimeLogging.Close()
			c.logging = nil
			c.initErr = fmt.Errorf("初始化 Enterprise Run 工作空间失败: %w", err)
			return
		}
		c.bundle, c.initErr = shared.BuildAgentService(ctx, shared.BuildOptions{
			Product:                    "enterprise",
			ConfigDir:                  configDir,
			Quiet:                      c.quiet,
			RouteName:                  "chat",
			DefaultAgentID:             "enterprise-default-agent",
			DefaultAgentName:           "Genesis Enterprise Agent",
			Profile:                    prof,
			RunWorkspace:               runWorkspace,
			AdditionalTools:            skillStack.Tools,
			PromptInjectors:            injectors,
			Logger:                     runtimeLogging.AgentLogger,
			AuditSink:                  auditSink,
			UsageSink:                  usageSink,
			SkillNameMatcher:           skillStack.SkillNameMatcher,
			SkillMentionSelector:       skillStack.SkillMentionSelector,
			SkillExplicitLoader:        skillStack.SkillExplicitLoader,
			SubAgentMaxConcurrent:      3,
			SubAgentProjection:         multiprojection.NewMemorySink(multiagentmodel.ProjectionChannelEnterprise),
			SubAgentCapabilityRegistry: capabilityRegistry,
			SubAgentApproval:           approvalSvc,
			HookApproval:               approvalSvc,
		})
		if c.initErr != nil {
			c.closeSkillStack()
			_ = runtimeLogging.Close()
			c.logging = nil
			return
		}
		if cfg.MCP.Enabled && c.bundle != nil && c.bundle.ToolGateway != nil {
			store, err := openEnterpriseApprovalStore()
			if err != nil {
				c.closeSkillStack()
				_ = runtimeLogging.Close()
				c.logging = nil
				c.bundle = nil
				c.initErr = fmt.Errorf("初始化Enterprise MCP approval store失败: %w", err)
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
				Requirements:       enterprisemcp.StdioDenyFilter{},
				Channel:            profilemodel.ChannelEnterprise,
				Environment:        profilemodel.EnvironmentServer,
				TenantID:           "dev",
				Workspace:          workspace,
				FailOnRequired:     true,
				AuditSink:          auditSink,
				Tracer:             c.bundle.Tracer,
			})
			if err != nil {
				c.closeSkillStack()
				_ = runtimeLogging.Close()
				c.logging = nil
				c.bundle = nil
				c.initErr = fmt.Errorf("初始化Enterprise MCP栈失败: %w", err)
				return
			}
			if mcpStack != nil && mcpStack.Authorizer != nil {
				c.bundle.ToolGateway.SetAuthorizer(mcpStack.Authorizer)
			}
			c.mcpStack = mcpStack
		}
		if err := toolvalidation.ValidateEnabled(c.bundle.ToolRegistry, prof.Tools.Enabled); err != nil {
			if c.mcpStack != nil {
				_ = c.mcpStack.Close(context.Background())
				c.mcpStack = nil
			}
			c.closeSkillStack()
			_ = runtimeLogging.Close()
			c.logging = nil
			c.bundle = nil
			c.initErr = fmt.Errorf("Enterprise 工具装配与 Profile 不一致: %w", err)
			return
		}
	})
	return c.initErr
}

// Service 返回初始化后的 AgentService。
func (c *Container) Service() app.AgentService {
	if c.bundle == nil {
		return nil
	}
	return c.bundle.AgentService
}

// MCPStack 返回已装配的 MCP 栈（可能为 nil）。
func (c *Container) MCPStack() *mcpstack.Stack { return c.mcpStack }

// SubAgentProjectionReader 返回经归约的子任务控制面投影，供 HTTP 审计面读取。
func (c *Container) SubAgentProjectionReader() multicontract.ProjectionReader {
	if c == nil || c.bundle == nil {
		return nil
	}
	reader, _ := c.bundle.SubAgentProjection.(multicontract.ProjectionReader)
	return reader
}

// Close 释放远端执行会话、MCP 连接与运行日志等资源。
func (c *Container) Close() error {
	if c == nil {
		return nil
	}
	var first error
	if err := c.closeSkillStack(); err != nil {
		first = err
	}
	if c.mcpStack != nil {
		if err := c.mcpStack.Close(context.Background()); err != nil && first == nil {
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

func (c *Container) closeSkillStack() error {
	if c == nil || c.skillStack == nil {
		return nil
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := c.skillStack.Close(closeCtx)
	cancel()
	c.skillStack = nil
	return err
}

type headlessAskApprover struct{}

// headlessAskApprover 用于无交互 HTTP 服务：PolicyAsk 自动批准（session 范围），PolicyDeny 仍拒绝。
// 生产环境应替换为人工审批 / RBAC requester；当前保证 Skill 脚本栈可跑通并写入审计。
func (headlessAskApprover) RequestApproval(ctx context.Context, req approvalmodel.Request, result approvalmodel.PolicyResult) (approvalmodel.Decision, error) {
	if err := ctx.Err(); err != nil {
		return approvalmodel.Decision{}, err
	}
	switch result.Type {
	case approvalmodel.PolicyDeny:
		return approvalmodel.Decision{Type: approvalmodel.DecisionDenied, Reason: firstNonEmpty(result.Reason, "policy deny")}, nil
	case approvalmodel.PolicyAllow:
		return approvalmodel.Decision{Type: approvalmodel.DecisionApproved, Reason: firstNonEmpty(result.Reason, "policy allow")}, nil
	default:
		return approvalmodel.Decision{
			Type:   approvalmodel.DecisionApprovedForScope,
			Scope:  approvalmodel.GrantScopeSession,
			Reason: firstNonEmpty(result.Reason, "enterprise headless auto-approve ask"),
		}, nil
	}
}

func buildEnterpriseApproval(policyCfg platformconfig.PolicyConfig, log logger.Logger, auditSink auditcontract.Sink) (approvalcontract.Service, error) {
	policyEngine, err := policyapproval.NewEngine(policyconfig.BuildEvaluator(policyCfg))
	if err != nil {
		return nil, fmt.Errorf("初始化Enterprise PolicyEngine失败: %w", err)
	}
	return approvalservice.New(policyEngine, headlessAskApprover{}, approvalmemory.NewStore(), log, approvalservice.WithAuditSink(auditSink))
}

func buildEnterpriseRunWorkspace(workspaceID string, manifests workspacecontract.RunManifestStore, artifactRuns artifactcontract.RunInitializer, completion artifactcontract.CompletionPolicy, qaEvidence artifactcontract.QAEvidenceRecorder) (app.RunWorkspaceRuntime, error) {
	if manifests == nil {
		return app.RunWorkspaceRuntime{}, fmt.Errorf("Enterprise 租户 RunManifestStore 未配置")
	}
	ids := idgen.NewUUIDGenerator()
	resolver, err := workservice.NewWorkspaceResolver(ids)
	if err != nil {
		return app.RunWorkspaceRuntime{}, err
	}
	rootID := "enterprise-runtime"
	if strings.TrimSpace(workspaceID) != "" {
		rootID = "enterprise-workspace:" + workspaceID
	}
	stateRoots := workspaceadapter.StateRootResolver{Root: workmodel.StateRoot{ID: rootID, Authority: "executor"}}
	preparer, err := workservice.NewRunPreparer(workservice.RunPreparerDeps{IDs: ids, Resolver: resolver, StateRoots: stateRoots, Provisioner: workspaceadapter.NewProvisioner(), Manifests: manifests})
	if err != nil {
		return app.RunWorkspaceRuntime{}, err
	}
	modes := []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject, execmodel.WorkspaceModeTask, execmodel.WorkspaceModeSession}
	appProfile := agentappmodel.EffectiveProfile{ID: "enterprise-default", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: modes, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	appResolver, err := agentappmemory.NewResolver(appProfile.ID, []agentappmodel.EffectiveProfile{appProfile})
	if err != nil {
		return app.RunWorkspaceRuntime{}, err
	}
	return app.RunWorkspaceRuntime{Preparer: preparer, AgentApps: appResolver, IntentResolver: workservice.NewTaskIntentResolver(), ProductModes: modes, BackendModes: modes, MaximumAccess: execmodel.WorkspaceAccessReadWrite, ArtifactRuns: artifactRuns, Completion: completion, QAEvidence: qaEvidence}, nil
}

// buildEnterpriseExecStack 只允许远程受治理执行；未配置时保留拒绝型 runner 使服务可启动，
// 但任何命令都明确失败，绝不回退到服务器实例本地进程或文件系统。
func buildEnterpriseExecStack(cfg platformconfig.SandboxConfig, log logger.Logger) (skillstack.ExecStack, error) {
	directRunner := enterpriseDeniedRunner{}
	var shellProvider execcontract.ShellCapabilityProvider
	profile := execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled}
	var sessionClient sandboxcontract.SessionClient
	var fileClient sandboxcontract.FileSystemClient
	var workspaceRef sandboxcontract.WorkspaceRef
	var sandboxRunner execcontract.SandboxRunner

	if cfg.Enabled {
		switch strings.ToLower(strings.TrimSpace(cfg.DefaultExecution)) {
		case "optional":
			profile.Mode = execmodel.SandboxOptional
		case "required":
			profile.Mode = execmodel.SandboxRequired
		default:
			profile.Mode = execmodel.SandboxDisabled
		}
		mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
		switch mode {
		case "docker_sandbox", "remote_sandbox":
			profile.Provider = "genesis-sandbox"
			profile.WorkspaceID = strings.TrimSpace(cfg.WorkspaceID)
			profile.Mode = execmodel.SandboxRequired
			client, err := sandboxhttp.New(sandboxhttp.Config{
				BaseURL: cfg.BaseURL,
				APIKey:  cfg.APIKey,
			})
			if err != nil {
				return skillstack.ExecStack{}, fmt.Errorf("初始化genesis-sandbox client失败: %w", err)
			}
			workspaceRef = sandboxcontract.WorkspaceRef{ID: cfg.WorkspaceID, Provider: "genesis-sandbox"}
			runner, err := execsandbox.NewRunner(client, workspaceRef)
			if err != nil {
				return skillstack.ExecStack{}, err
			}
			sandboxRunner = runner
			sessionClient = client
			fileClient = client
		case "local_platform_sandbox":
			return skillstack.ExecStack{}, fmt.Errorf("Enterprise 禁止 local_platform_sandbox；必须配置远程 genesis-sandbox")
		default:
			return skillstack.ExecStack{}, fmt.Errorf("Enterprise 不支持 sandbox mode %q", cfg.Mode)
		}
		if strings.TrimSpace(cfg.DefaultRuntimeProfile) != "" {
			profile.RuntimeProfile = execmodel.SandboxRuntimeProfile(cfg.DefaultRuntimeProfile)
		}
	}

	executionRunner, err := execservice.NewRunner(directRunner, sandboxRunner, execservice.WithLogger(log))
	if err != nil {
		return skillstack.ExecStack{}, err
	}
	return skillstack.ExecStack{
		Runner:        executionRunner,
		Shells:        shellProvider,
		SessionClient: sessionClient,
		FileClient:    fileClient,
		WorkspaceRef:  workspaceRef,
		Sandbox:       profile,
	}, nil
}

type enterpriseDeniedRunner struct{}

func (enterpriseDeniedRunner) Run(context.Context, execmodel.Command, execcontract.RunOptions) (*execmodel.Result, error) {
	return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("Enterprise 未配置远程受治理执行器，拒绝服务器本地执行"))
}

func enterpriseEnvironmentContext(ctx context.Context, stack skillstack.ExecStack) promptbuilder.EnvironmentContext {
	environment := promptbuilder.EnvironmentContext{
		OS:               runtime.GOOS,
		SandboxMode:      string(stack.Sandbox.Mode),
		SandboxProvider:  stack.Sandbox.Provider,
		ExternalApproval: true,
	}
	if stack.Shells == nil {
		if stack.Sandbox.Provider == "genesis-sandbox" {
			environment.OS = ""
		}
		return environment
	}
	capabilities := stack.Shells.ShellCapabilities(ctx)
	environment.DefaultShell = string(capabilities.Default.Kind)
	environment.DefaultShellPath = capabilities.Default.Path
	for _, shell := range capabilities.Supported {
		if shell.Kind != "" {
			environment.SupportedShells = append(environment.SupportedShells, string(shell.Kind))
		}
	}
	return environment
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
