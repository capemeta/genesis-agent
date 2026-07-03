// Package bootstrap 装配 Genesis CLI 产品入口。
package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"genesis-agent/internal/app"
	shared "genesis-agent/internal/bootstrap"
	approvaldeny "genesis-agent/internal/capabilities/approval/adapter/deny"
	approvalmemory "genesis-agent/internal/capabilities/approval/adapter/memory"
	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalservice "genesis-agent/internal/capabilities/approval/service"
	auditmemory "genesis-agent/internal/capabilities/audit/adapter/memory"
	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	execservice "genesis-agent/internal/capabilities/execution/service"
	runcommand "genesis-agent/internal/capabilities/execution/tool/run_command"
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
	policyapproval "genesis-agent/internal/capabilities/policy/adapter/approval"
	policyconfig "genesis-agent/internal/capabilities/policy/adapter/config"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	skillmemory "genesis-agent/internal/capabilities/skill/adapter/memory"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	loadskill "genesis-agent/internal/capabilities/skill/tool/load_skill"
	readskillresource "genesis-agent/internal/capabilities/skill/tool/read_skill_resource"
	searchskillresources "genesis-agent/internal/capabilities/skill/tool/search_skill_resources"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
	usagememory "genesis-agent/internal/capabilities/usage/adapter/memory"
	usagecontract "genesis-agent/internal/capabilities/usage/contract"
	platformconfig "genesis-agent/internal/platform/config"
	promptbuilder "genesis-agent/internal/runtime/prompt"
	cliapproval "genesis-agent/products/cli/internal/approval"
	"genesis-agent/products/cli/internal/command"
	"genesis-agent/products/cli/internal/profile"
	clisandbox "genesis-agent/products/cli/internal/sandbox"
	localexec "genesis-agent/shared/local/execution"
	localfs "genesis-agent/shared/local/filesystem"
	localresolver "genesis-agent/shared/local/pathresolver"
	localskill "genesis-agent/shared/local/skill"
)

// Container 是 CLI 产品的装配容器。
type Container struct {
	configDirRef *string
	quiet        bool
	sandbox      clisandbox.Config

	once    sync.Once
	initErr error
	bundle  *shared.RuntimeBundle
}

type productRuntime struct {
	tools           []toolcontract.Tool
	promptInjectors []promptbuilder.ContextInjector
	auditSink       auditcontract.Sink
	usageSink       usagecontract.Sink
}

// Execute 执行 CLI 产品命令树。
func Execute(ctx context.Context) error {
	return command.ExecuteWithFactory(func(runCtx context.Context, opts command.ServiceOptions) (app.AgentService, error) {
		if runCtx == nil {
			runCtx = ctx
		}
		return NewServiceWithOptions(runCtx, opts)
	})
}

// NewContainer 创建 CLI 产品容器。
func NewContainer(configDirRef *string, quiet bool) *Container {
	return &Container{configDirRef: configDirRef, quiet: quiet, sandbox: clisandbox.DefaultConfig()}
}

// Init 初始化 CLI 产品运行时依赖。
func (c *Container) Init(ctx context.Context) error {
	c.once.Do(func() {
		configDir := ""
		if c.configDirRef != nil {
			configDir = *c.configDirRef
		}
		prof := profile.DefaultProfile()
		runtime, err := buildProductRuntime(ctx, configDir, c.quiet, c.sandbox, prof)
		if err != nil {
			c.initErr = err
			return
		}
		c.bundle, c.initErr = shared.BuildAgentService(ctx, shared.BuildOptions{ConfigDir: configDir, Quiet: c.quiet, RouteName: "chat", DefaultAgentID: "default-agent", DefaultAgentName: "Genesis Agent", Profile: prof, AdditionalTools: runtime.tools, PromptInjectors: runtime.promptInjectors, AuditSink: runtime.auditSink, UsageSink: runtime.usageSink})
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

func NewService(ctx context.Context, configDirRef *string, quiet bool) (app.AgentService, error) {
	return NewServiceWithOptions(ctx, command.ServiceOptions{ConfigDirRef: configDirRef, Quiet: quiet, Sandbox: clisandbox.DefaultConfig()})
}

func NewServiceWithOptions(ctx context.Context, opts command.ServiceOptions) (app.AgentService, error) {
	sandboxCfg := opts.Sandbox
	if sandboxCfg.Mode == "" && sandboxCfg.Execution == "" {
		sandboxCfg = clisandbox.DefaultConfig()
	}
	c := &Container{configDirRef: opts.ConfigDirRef, quiet: opts.Quiet, sandbox: sandboxCfg}
	if err := c.Init(ctx); err != nil {
		return nil, err
	}
	return c.Service(), nil
}

func buildProductRuntime(ctx context.Context, configDir string, quiet bool, sandboxCfg clisandbox.Config, prof profilemodel.Profile) (productRuntime, error) {
	cfg, err := platformconfig.Load(configDir)
	if err != nil {
		return productRuntime{}, fmt.Errorf("加载CLI产品配置失败: %w", err)
	}
	auditSink := auditmemory.NewSink()
	usageSink := usagememory.NewSink()
	baseApprovalSvc, err := buildBaseApprovalService(quiet, cfg.Policy)
	if err != nil {
		return productRuntime{}, err
	}
	tools, err := buildProductTools(sandboxCfg, baseApprovalSvc)
	if err != nil {
		return productRuntime{}, err
	}
	skillSvc, roots, err := buildSkillService(configDir, prof, auditSink, usageSink)
	if err != nil {
		return productRuntime{}, err
	}
	startSkillWatcher(ctx, roots, skillSvc)
	catalogReq := skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI, Environment: profilemodel.EnvironmentLocal, EnabledSkills: prof.Skills.Enabled, DisabledSkills: prof.Skills.Disabled}
	loadSkill, err := loadskill.New(loadskill.Deps{Service: skillSvc, Approval: baseApprovalSvc, CatalogRequest: catalogReq, EnabledTools: toolNames(tools)})
	if err != nil {
		return productRuntime{}, err
	}
	readResource, err := readskillresource.New(readskillresource.Deps{Service: skillSvc, Approval: baseApprovalSvc, CatalogRequest: catalogReq})
	if err != nil {
		return productRuntime{}, err
	}
	searchResources, err := searchskillresources.New(searchskillresources.Deps{Service: skillSvc, Approval: baseApprovalSvc, CatalogRequest: catalogReq})
	if err != nil {
		return productRuntime{}, err
	}
	tools = append(tools, loadSkill, readResource, searchResources)
	injector := promptbuilder.ContextInjectorFunc(func(ctx context.Context, req promptbuilder.BuildRequest) (promptbuilder.Fragment, error) {
		contents, err := skillSvc.RenderAvailableSkills(ctx, catalogReq)
		if err != nil {
			return promptbuilder.Fragment{}, err
		}
		return promptbuilder.Fragment{Name: "skills_instructions", Contents: contents}, nil
	})
	return productRuntime{tools: tools, promptInjectors: []promptbuilder.ContextInjector{injector}, auditSink: auditSink, usageSink: usageSink}, nil
}

func toolNames(tools []toolcontract.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, candidate := range tools {
		if candidate == nil || candidate.GetInfo() == nil {
			continue
		}
		names = append(names, candidate.GetInfo().Name)
	}
	return names
}
func buildBaseApprovalService(quiet bool, policyCfg platformconfig.PolicyConfig) (approvalcontract.Service, error) {
	policyEngine, err := policyapproval.NewEngine(policyconfig.BuildEvaluator(policyCfg))
	if err != nil {
		return nil, fmt.Errorf("初始化PolicyEngine失败: %w", err)
	}
	baseApprovalSvc, err := approvalservice.New(policyEngine, newApprovalRequester(quiet), approvalmemory.NewStore())
	if err != nil {
		return nil, fmt.Errorf("初始化ApprovalService失败: %w", err)
	}
	return baseApprovalSvc, nil
}
func buildProductTools(sandboxCfg clisandbox.Config, baseApprovalSvc approvalcontract.Service) ([]toolcontract.Tool, error) {
	resolver, err := localresolver.New("")
	if err != nil {
		return nil, fmt.Errorf("初始化本地PathResolver失败: %w", err)
	}
	approvalSvc := fspermission.NewApprovalService(baseApprovalSvc, fspermission.NewRuntimeFilePermissions())
	locker := scheduler.NewMemoryResourceLocker()
	fileDeps := toolkit.Deps{Resolver: resolver, Backend: localfs.New(), Approval: approvalSvc, Freshness: freshness.NewMemoryTracker(), Locker: locker}
	constructors := []func(toolkit.Deps) (toolcontract.Tool, error){readfile.New, writefile.New, editfile.New, applypatchtool.New, listdir.New, walkdir.New, globtool.New, greptool.New}
	tools := make([]toolcontract.Tool, 0, len(constructors)+1)
	for _, constructor := range constructors {
		t, err := constructor(fileDeps)
		if err != nil {
			return nil, err
		}
		tools = append(tools, t)
	}
	directRunner := localexec.NewRunner()
	localSandboxRunner, err := localexec.NewSandboxRunner(directRunner, localexec.SandboxRunnerOptions{})
	if err != nil {
		return nil, err
	}
	executionRunner, err := execservice.NewRunner(directRunner, localSandboxRunner)
	if err != nil {
		return nil, err
	}
	runCommand, err := runcommand.New(runcommand.Deps{Runner: executionRunner, Resolver: resolver, Approval: approvalSvc, Locker: locker, Sandbox: sandboxCfg.ExecutionProfile()})
	if err != nil {
		return nil, err
	}
	tools = append(tools, runCommand)
	return tools, nil
}

func buildSkillService(configDir string, prof profilemodel.Profile, auditSink auditcontract.Sink, usageSink usagecontract.Sink) (skillcontract.Service, []localskill.Root, error) {
	roots := defaultSkillRoots(configDir)
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
	systemSource := skillmemory.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "cli-system"}, nil)
	return skillservice.New([]skillcontract.Source{systemSource, source}, skillservice.Options{AuditSink: auditSink, UsageSink: usageSink}), roots, nil
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

func defaultSkillRoots(configDir string) []localskill.Root {
	roots := make([]localskill.Root, 0, 3)
	if wd, err := os.Getwd(); err == nil && wd != "" {
		roots = append(roots, localskill.Root{Path: filepath.Join(wd, ".genesis", "skills"), Scope: skillmodel.ScopeProject})
	}
	if configDir != "" {
		roots = append(roots, localskill.Root{Path: filepath.Join(configDir, "skills"), Scope: skillmodel.ScopeUser})
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, localskill.Root{Path: filepath.Join(home, ".genesis", "skills"), Scope: skillmodel.ScopeUser})
	}
	return dedupeRoots(roots)
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
		return approvaldeny.NewRequester()
	}
	return cliapproval.NewTerminalRequester(os.Stdin, os.Stderr)
}
