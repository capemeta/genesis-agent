// Package bootstrap 装配 Genesis CLI 产品入口。
package bootstrap

import (
	"context"
	"fmt"
	"os"
	"sync"

	"genesis-agent/internal/app"
	shared "genesis-agent/internal/bootstrap"
	approvaldeny "genesis-agent/internal/capabilities/approval/adapter/deny"
	approvalmemory "genesis-agent/internal/capabilities/approval/adapter/memory"
	approvalstatic "genesis-agent/internal/capabilities/approval/adapter/static"
	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalservice "genesis-agent/internal/capabilities/approval/service"
	"genesis-agent/internal/capabilities/filesystem/freshness"
	editfile "genesis-agent/internal/capabilities/filesystem/tool/edit_file"
	listdir "genesis-agent/internal/capabilities/filesystem/tool/list_dir"
	readfile "genesis-agent/internal/capabilities/filesystem/tool/read_file"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	walkdir "genesis-agent/internal/capabilities/filesystem/tool/walk_dir"
	writefile "genesis-agent/internal/capabilities/filesystem/tool/write_file"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
	cliapproval "genesis-agent/products/cli/internal/approval"
	"genesis-agent/products/cli/internal/command"
	"genesis-agent/products/cli/internal/profile"
	localfs "genesis-agent/shared/local/filesystem"
	localresolver "genesis-agent/shared/local/pathresolver"
)

// Container 是 CLI 产品的装配容器。
type Container struct {
	configDirRef *string
	quiet        bool

	once    sync.Once
	initErr error
	bundle  *shared.RuntimeBundle
}

// Execute 执行 CLI 产品命令树。
func Execute(ctx context.Context) error {
	return command.ExecuteWithFactory(func(runCtx context.Context, configDirRef *string, quiet bool) (app.AgentService, error) {
		if runCtx == nil {
			runCtx = ctx
		}
		return NewService(runCtx, configDirRef, quiet)
	})
}

// NewContainer 创建 CLI 产品容器。
func NewContainer(configDirRef *string, quiet bool) *Container {
	return &Container{configDirRef: configDirRef, quiet: quiet}
}

// Init 初始化 CLI 产品运行时依赖。
func (c *Container) Init(ctx context.Context) error {
	c.once.Do(func() {
		configDir := ""
		if c.configDirRef != nil {
			configDir = *c.configDirRef
		}
		additionalTools, err := buildFileTools(c.quiet)
		if err != nil {
			c.initErr = err
			return
		}
		c.bundle, c.initErr = shared.BuildAgentService(ctx, shared.BuildOptions{
			ConfigDir:        configDir,
			Quiet:            c.quiet,
			RouteName:        "chat",
			DefaultAgentID:   "default-agent",
			DefaultAgentName: "Genesis Agent",
			Profile:          profile.DefaultProfile(),
			AdditionalTools:  additionalTools,
		})
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

// NewService 构建 CLI 产品 AgentService，供 CLI interface 注入使用。
func NewService(ctx context.Context, configDirRef *string, quiet bool) (app.AgentService, error) {
	c := NewContainer(configDirRef, quiet)
	if err := c.Init(ctx); err != nil {
		return nil, err
	}
	return c.Service(), nil
}

func buildFileTools(quiet bool) ([]toolcontract.Tool, error) {
	resolver, err := localresolver.New("")
	if err != nil {
		return nil, fmt.Errorf("初始化本地PathResolver失败: %w", err)
	}
	approvalSvc, err := approvalservice.New(
		approvalstatic.NewPolicyEngine(),
		newApprovalRequester(quiet),
		approvalmemory.NewStore(),
	)
	if err != nil {
		return nil, fmt.Errorf("初始化ApprovalService失败: %w", err)
	}
	deps := toolkit.Deps{
		Resolver:  resolver,
		Backend:   localfs.New(),
		Approval:  approvalSvc,
		Freshness: freshness.NewMemoryTracker(),
		Locker:    scheduler.NewMemoryResourceLocker(),
	}
	constructors := []func(toolkit.Deps) (toolcontract.Tool, error){
		readfile.New,
		writefile.New,
		editfile.New,
		listdir.New,
		walkdir.New,
	}
	tools := make([]toolcontract.Tool, 0, len(constructors))
	for _, constructor := range constructors {
		t, err := constructor(deps)
		if err != nil {
			return nil, err
		}
		tools = append(tools, t)
	}
	return tools, nil
}

func newApprovalRequester(quiet bool) approvalcontract.Requester {
	if quiet {
		return approvaldeny.NewRequester()
	}
	return cliapproval.NewTerminalRequester(os.Stdin, os.Stderr)
}
