// Package bootstrap 装配 Genesis Desktop 产品入口。
package bootstrap

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// Execute 是 Desktop 产品入口：先完成内核/MCP 装配校验，UI（Wails）仍待实现。
func Execute(ctx context.Context) error {
	return newRootCmd(ctx).Execute()
}

func newRootCmd(parent context.Context) *cobra.Command {
	var configDir string
	cmd := &cobra.Command{
		Use:           "genesis-desktop",
		Short:         "Genesis Agent Desktop（内核已可装配，Wails UI 待实现）",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewContainer(configDir, false)
			if err := c.Init(parent); err != nil {
				return err
			}
			defer func() { _ = c.Close() }()

			mcpReady := c.MCPStack() != nil && c.MCPStack().Manager != nil
			return fmt.Errorf("genesis-desktop Wails UI 暂未实现；内核装配成功（mcp_stack_ready=%v）", mcpReady)
		},
	}
	cmd.Flags().StringVarP(&configDir, "config", "c", "configs", "配置目录路径")
	return cmd
}
