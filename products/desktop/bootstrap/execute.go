// Package bootstrap 装配 Genesis Desktop 产品入口。
package bootstrap

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// Execute 是 Desktop 产品入口：内核/MCP 可装配；`run --attach` 提供最小附件交互；Wails UI 仍待实现。
func Execute(ctx context.Context) error {
	return newRootCmd(ctx).Execute()
}

func newRootCmd(parent context.Context) *cobra.Command {
	var configDir string
	cmd := &cobra.Command{
		Use:           "genesis-desktop",
		Short:         "Genesis Agent Desktop（内核可装配；run --attach 最小附件；Wails UI 待实现）",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewContainer(configDir, false)
			if err := c.Init(parent); err != nil {
				return err
			}
			defer func() { _ = c.Close() }()

			mcpReady := c.MCPStack() != nil && c.MCPStack().Manager != nil
			return fmt.Errorf("genesis-desktop Wails UI 暂未实现；内核装配成功（mcp_stack_ready=%v）；可用子命令: run --attach", mcpReady)
		},
	}
	cmd.Flags().StringVarP(&configDir, "config", "c", "configs", "配置目录路径")
	addRunCommand(cmd, parent)
	return cmd
}
