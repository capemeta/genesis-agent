package command

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	windowssandbox "genesis-agent/shared/local/sandbox/windows"
)

func newSandboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "sandbox",
		Short:        "管理本地沙箱环境（windows-setup）",
		SilenceUsage: true,
	}
	cmd.AddCommand(
		newSandboxWindowsSetupCmd(),
	)
	return cmd
}

func newSandboxWindowsSetupCmd() *cobra.Command {
	var setupNetwork bool
	var appDataDir string
	cmd := &cobra.Command{
		Use:   "windows-setup",
		Short: "在 Windows 平台初始化本地沙箱所需环境 (ACL/Readiness)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "windows" {
				return fmt.Errorf("windows-setup 仅在 Windows 操作系统支持")
			}
			if appDataDir != "" {
				windowssandbox.SetSandboxDirOverride(appDataDir)
			}
			return windowssandbox.RunWindowsSetupWithFlags(setupNetwork)
		},
	}
	cmd.Flags().BoolVar(&setupNetwork, "network", false, "是否配置 Windows 本地受限账户与网络隔离防火墙规则 (需管理员权限)")
	cmd.Flags().StringVar(&appDataDir, "appdata", "", "指定真实用户的 AppData 目录，提权执行时使用")
	return cmd
}
