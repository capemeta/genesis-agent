package command

import (
	"context"
	"fmt"
	"os"

	"genesis-agent/products/cli/internal/tui/chat"
	cliapproval "genesis-agent/products/cli/internal/approval"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// newChatCmd 创建 chat 子命令
// 启动基于 Bubble Tea + Lip Gloss 的全功能 TUI 对话界面
func newChatCmd(configDirRef *string, sandboxModeRef *string, factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "启动交互式 TUI 对话（推荐）",
		Long: `启动基于 Bubble Tea + Lip Gloss 的全功能终端对话界面。

界面布局:
  ┌ 标题栏 ─ 显示模型、策略、会话信息 ────────────────────┐
  │ 消息区 ─ 可滚动的对话历史                             │
  │ 状态栏 ─ 推理中显示动画，有错误时显示错误摘要          │
  │ 帮助栏 ─ 常用快捷键提示                               │
  └ 输入框 ─ 实时输入，Enter 发送 ──────────────────────────┘

快捷键:
  Enter      发送消息
  Ctrl+C     退出程序
  Esc        清空输入框
  ↑ / ↓      滚动消息历史
  PgUp/PgDn  快速翻页
  鼠标拖选   可直接选中并复制文字（未启用鼠标捕获）

内置命令（以 / 开头）:
  /clear     清空当前会话历史，开始新对话
  /help      显示帮助信息
  /quit      退出程序`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// TUI 模式：禁用 stdout 日志/Trace，避免污染 Bubble Tea 画面
			svc, err := initService(ctx, factory, configDirRef, true, sandboxModeRef)
			if err != nil {
				return fmt.Errorf("初始化失败: %w\n\n请检查配置文件或 API Key 是否正确", err)
			}

			session := svc.NewSession()

			// 构建 Bubble Tea 初始 Model
			m := chat.NewModel(ctx, svc, session)

			// 启动 TUI 程序
			// WithAltScreen: 使用备用屏幕，退出后恢复原始终端内容
			// 故意不启用 WithMouseCellMotion：鼠标捕获会接管终端选区，
			// 导致无法拖选复制文字；消息历史请用 ↑↓ / PgUp/PgDn 滚动。
			p := tea.NewProgram(
				m,
				tea.WithAltScreen(),
			)

			// 绑定全局 TUI 审批器对应的 program 实例
			cliapproval.GlobalTUIRequester.SetProgram(p)

			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "TUI 运行错误: %v\n", err)
				return err
			}

			return nil
		},
	}

	return cmd
}
