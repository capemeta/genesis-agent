package command

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// newToolsCmd 创建 tools 子命令
// tools 命令列出所有已注册的工具及其描述，方便用户了解 Agent 可用能力
func newToolsCmd(configDirRef *string, factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "列出已注册的可用工具",
		Long:  `显示 Agent 可调用的所有工具，包括工具名称和功能描述。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// tools 命令不需要 TUI，保持 stdout 输出
			svc, err := initService(ctx, factory, configDirRef, false)
			if err != nil {
				return fmt.Errorf("初始化失败: %w", err)
			}

			infos := svc.ListTools()

			titleStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7C3AED")).
				Bold(true)
			nameStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8B5CF6")).
				Bold(true).
				Width(22)
			descStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#D1D5DB"))
			countStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#9CA3AF"))

			fmt.Println()
			fmt.Printf("%s %s\n\n",
				titleStyle.Render("已注册工具"),
				countStyle.Render(fmt.Sprintf("(%d 个)", len(infos))),
			)

			for _, info := range infos {
				fmt.Printf("  %s %s\n",
					nameStyle.Render(info.Name),
					descStyle.Render(info.Description),
				)
			}
			fmt.Println()

			return nil
		},
	}

	return cmd
}
