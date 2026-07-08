package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
	cliskill "genesis-agent/products/cli/internal/skill"
)

func newPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "安装和管理组合 Package Plugin",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newPluginInstallCmd(),
		newPluginListCmd(),
		newPluginShowCmd(),
		newPluginEnableCmd(true),
		newPluginEnableCmd(false),
		newPluginUninstallCmd(),
	)
	return cmd
}

func newPluginInstallCmd() *cobra.Command {
	var scope string
	var force bool
	cmd := &cobra.Command{
		Use:   "install <plugin[@marketplace]>",
		Short: "安装 Plugin 组合包",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			record, err := svc.InstallPlugin(cmd.Context(), args[0], marketmodel.InstallScope(scope), force)
			if err != nil {
				return err
			}
			fmt.Printf("已安装 plugin %s，capabilities=%d\n", record.Spec, len(record.Capabilities))
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", string(marketmodel.InstallScopeUser), "安装范围：user 或 project")
	cmd.Flags().BoolVar(&force, "force", false, "覆盖已安装 Plugin")
	return cmd
}

func newPluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出已安装 Plugin",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			plugins, err := svc.ListPlugins(cmd.Context())
			if err != nil {
				return err
			}
			for _, plugin := range plugins {
				state := "disabled"
				if plugin.Install.Enabled {
					state = "enabled"
				}
				fmt.Printf("%s\t%s\t%s\tcapabilities=%d\n", plugin.Install.Spec, plugin.Install.Scope, state, len(plugin.Capabilities))
			}
			return nil
		},
	}
}

func newPluginShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <plugin@marketplace>",
		Short: "查看已安装 Plugin 详情",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			plugin, err := svc.Plugin(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			state := "disabled"
			if plugin.Install.Enabled {
				state = "enabled"
			}
			fmt.Printf("Name: %s\nMarketplace: %s\nVersion: %s\nScope: %s\nState: %s\nInstallRoot: %s\n", plugin.Install.Package, plugin.Install.Marketplace, plugin.Install.Version, plugin.Install.Scope, state, plugin.Install.InstallRoot)
			if len(plugin.Capabilities) > 0 {
				lines := make([]string, 0, len(plugin.Capabilities))
				for _, capability := range plugin.Capabilities {
					lines = append(lines, fmt.Sprintf("%s:%s:%s", capability.Type, capability.Name, capability.ResourcePath))
				}
				fmt.Printf("Capabilities:\n  %s\n", strings.Join(lines, "\n  "))
			}
			return nil
		},
	}
}

func newPluginEnableCmd(enabled bool) *cobra.Command {
	use := "enable <plugin@marketplace>"
	short := "启用已安装 Plugin"
	if !enabled {
		use = "disable <plugin@marketplace>"
		short = "禁用已安装 Plugin"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			plugin, err := svc.Plugin(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			record, err := svc.SetEnabled(cmd.Context(), plugin.Install.Spec, enabled)
			if err != nil {
				return err
			}
			fmt.Printf("plugin %s 已%s\n", record.Spec, map[bool]string{true: "启用", false: "禁用"}[enabled])
			return nil
		},
	}
}

func newPluginUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <plugin@marketplace>",
		Short: "卸载 Plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := cliskill.NewMarketplaceService()
			if err != nil {
				return err
			}
			plugin, err := svc.Plugin(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := svc.Uninstall(cmd.Context(), plugin.Install.Spec); err != nil {
				return err
			}
			fmt.Printf("已卸载 plugin %s\n", plugin.Install.Spec)
			return nil
		},
	}
}
