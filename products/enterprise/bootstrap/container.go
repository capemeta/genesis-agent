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
	approvalmemory "genesis-agent/internal/capabilities/approval/adapter/memory"
	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	approvalservice "genesis-agent/internal/capabilities/approval/service"
	auditfile "genesis-agent/internal/capabilities/audit/adapter/file"
	auditmemory "genesis-agent/internal/capabilities/audit/adapter/memory"
	auditcontract "genesis-agent/internal/capabilities/audit/contract"
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
	usagefile "genesis-agent/internal/capabilities/usage/adapter/file"
	usagememory "genesis-agent/internal/capabilities/usage/adapter/memory"
	usagecontract "genesis-agent/internal/capabilities/usage/contract"
	platformconfig "genesis-agent/internal/platform/config"
	"genesis-agent/internal/platform/logger"
	multicontract "genesis-agent/internal/runtime/multiagent/contract"
	multiagentmodel "genesis-agent/internal/runtime/multiagent/model"
	multiprojection "genesis-agent/internal/runtime/multiagent/projection"
	promptbuilder "genesis-agent/internal/runtime/prompt"
	enterprisemcp "genesis-agent/products/enterprise/internal/mcp"
	"genesis-agent/products/enterprise/internal/profile"
	localexec "genesis-agent/shared/local/execution"
	"genesis-agent/shared/local/skillmarket"
	"genesis-agent/shared/skillstack"
)

func loadEnterpriseCapabilityIndex(adapters capcontract.RuntimeAdapterRegistry) (capcontract.Registry, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("无法定位用户主目录")
	}
	indexFile := filepath.Join(home, ".genesis-agent", "enterprise", "capability-index.json")
	return capservice.NewRegistry(capservice.Options{
		Store:    skillmarket.NewCapabilityIndexStore(indexFile),
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

	once     sync.Once
	initErr  error
	bundle   *shared.RuntimeBundle
	logging  *logger.RuntimeLogging
	mcpStack *mcpstack.Stack
}

// NewContainer 创建 Enterprise 产品容器。
func NewContainer(configDirRef *string, quiet bool) *Container {
	return &Container{configDirRef: configDirRef, quiet: quiet}
}

// Init 初始化 Enterprise 产品运行时依赖。
func (c *Container) Init(ctx context.Context) error {
	c.once.Do(func() {
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

		prof := profile.DefaultProfile()
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
			WorkspaceRoot: func() string {
				if wd, err := os.Getwd(); err == nil && wd != "" {
					return wd
				}
				return "."
			}(),
			Exec: execStack,
		})
		if err != nil {
			_ = runtimeLogging.Close()
			c.logging = nil
			c.initErr = fmt.Errorf("初始化Enterprise Skill栈失败: %w", err)
			return
		}

		injectors := make([]promptbuilder.ContextInjector, 0, 2)
		injectors = append(injectors, promptbuilder.NewEnvironmentContextInjector(enterpriseEnvironmentContext(ctx, execStack)))
		if skillStack.PromptInjector != nil {
			injectors = append(injectors, skillStack.PromptInjector)
		}
		c.bundle, c.initErr = shared.BuildAgentService(ctx, shared.BuildOptions{
			Product:                    "enterprise",
			ConfigDir:                  configDir,
			Quiet:                      c.quiet,
			RouteName:                  "chat",
			DefaultAgentID:             "enterprise-default-agent",
			DefaultAgentName:           "Genesis Enterprise Agent",
			Profile:                    prof,
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
			_ = runtimeLogging.Close()
			c.logging = nil
			return
		}
		if cfg.MCP.Enabled && c.bundle != nil && c.bundle.ToolGateway != nil {
			store, err := openEnterpriseApprovalStore()
			if err != nil {
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

// Close 释放 MCP 连接与运行日志等资源。
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

// NewService 构建 Enterprise 产品 AgentService，保持与 CLI bootstrap 一致的接口形态。
func NewService(ctx context.Context, configDirRef *string, quiet bool) (app.AgentService, error) {
	c := NewContainer(configDirRef, quiet)
	if err := c.Init(ctx); err != nil {
		return nil, err
	}
	return c.Service(), nil
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

// buildEnterpriseExecStack 按顶层 sandbox 配置装配执行栈（不再写死仅 disabled）。
// 无配置 / enabled=false → 本地 disabled；docker/remote + base_url → genesis-sandbox SessionClient。
// 生产 headless ask 审批仍为过渡方案（见 headlessAskApprover 注释）。
func buildEnterpriseExecStack(cfg platformconfig.SandboxConfig, log logger.Logger) (skillstack.ExecStack, error) {
	directRunner := localexec.NewRunner()
	var shellProvider execcontract.ShellCapabilityProvider = directRunner
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
			shellProvider = nil
			profile.Provider = "genesis-sandbox"
			profile.WorkspaceID = strings.TrimSpace(cfg.WorkspaceID)
			if profile.Mode == execmodel.SandboxDisabled {
				profile.Mode = execmodel.SandboxRequired
			}
			client, err := sandboxhttp.New(sandboxhttp.Config{
				BaseURL:           cfg.BaseURL,
				APIKey:            cfg.APIKey,
				Timeout:           cfg.Timeout,
				LocalArtifactRoot: filepath.Join(".", ".genesis", "artifacts"),
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
			profile.Provider = "local-platform"
			if profile.Mode == execmodel.SandboxDisabled {
				profile.Mode = execmodel.SandboxOptional
			}
			runner, err := localexec.NewSandboxRunner(directRunner, localexec.SandboxRunnerOptions{})
			if err != nil {
				return skillstack.ExecStack{}, err
			}
			sandboxRunner = runner
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

func enterpriseEnvironmentContext(ctx context.Context, stack skillstack.ExecStack) promptbuilder.EnvironmentContext {
	cwd, _ := os.Getwd()
	environment := promptbuilder.EnvironmentContext{
		OS:               runtime.GOOS,
		Cwd:              cwd,
		SandboxMode:      string(stack.Sandbox.Mode),
		SandboxProvider:  stack.Sandbox.Provider,
		ExternalApproval: true,
	}
	if stack.Shells == nil {
		if stack.Sandbox.Provider == "genesis-sandbox" {
			environment.OS = ""
			environment.Cwd = "/workspace"
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
