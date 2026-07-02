package command

import (
	"fmt"
	"runtime"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// 版本信息常量，可通过编译时 -ldflags 注入真实值：
//
//	go build -ldflags "-X genesis-agent/products/cli/internal/command.Version=1.0.0 \
//	                   -X genesis-agent/products/cli/internal/command.GitCommit=$(git rev-parse --short HEAD) \
//	                   -X genesis-agent/products/cli/internal/command.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
//	         ./cmd/genesis-cli
var (
	// Version 语义化版本号
	Version = "0.1.0-dev"
	// GitCommit 构建时的 Git commit hash（短格式）
	GitCommit = "unknown"
	// BuildTime 构建时间（ISO 8601 UTC）
	BuildTime = "unknown"
)

// newVersionCmd 创建 version 子命令
func newVersionCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "显示版本信息",
		Long:  `显示 Genesis Agent 的版本号、构建信息和运行环境。`,
		Run: func(cmd *cobra.Command, args []string) {
			titleStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8B5CF6")).
				Bold(true)
			valueStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F9FAFB"))
			labelStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#9CA3AF")).
				Width(14)

			fmt.Println()
			fmt.Printf("%s %s\n",
				titleStyle.Render("Genesis Agent"),
				valueStyle.Render("v"+Version),
			)

			if verbose {
				fmt.Println()
				row := func(label, value string) {
					fmt.Printf("  %s %s\n", labelStyle.Render(label), valueStyle.Render(value))
				}
				row("Git Commit", GitCommit)
				row("Build Time", BuildTime)
				row("Go Version", runtime.Version())
				row("OS / Arch", fmt.Sprintf("%s / %s", runtime.GOOS, runtime.GOARCH))
			}
			fmt.Println()
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "显示详细版本信息（Git commit、构建时间、运行环境）")

	return cmd
}
