// Package bootstrap 装配 Genesis Enterprise 产品入口。
package bootstrap

import (
	"context"
	"sync"

	"genesis-agent/internal/app"
	shared "genesis-agent/internal/bootstrap"
	"genesis-agent/products/enterprise/internal/profile"
)

// Container 是 Enterprise 产品的装配容器。
type Container struct {
	configDirRef *string
	quiet        bool

	once    sync.Once
	initErr error
	bundle  *shared.RuntimeBundle
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
		c.bundle, c.initErr = shared.BuildAgentService(ctx, shared.BuildOptions{
			ConfigDir:        configDir,
			Quiet:            c.quiet,
			RouteName:        "chat",
			DefaultAgentID:   "enterprise-default-agent",
			DefaultAgentName: "Genesis Enterprise Agent",
			Profile:          profile.DefaultProfile(),
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

// NewService 构建 Enterprise 产品 AgentService，保持与 CLI bootstrap 一致的接口形态。
func NewService(ctx context.Context, configDirRef *string, quiet bool) (app.AgentService, error) {
	c := NewContainer(configDirRef, quiet)
	if err := c.Init(ctx); err != nil {
		return nil, err
	}
	return c.Service(), nil
}
