package command

import (
	"context"
	"fmt"
	"os"
	"strings"

	"genesis-agent/internal/app"
	"genesis-agent/internal/domain"
	cliapproval "genesis-agent/products/cli/internal/approval"
	clitui "genesis-agent/products/cli/internal/tui"
	"genesis-agent/products/cli/internal/tui/chat"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// newChatCmd 创建 chat 子命令
// 启动基于 Bubble Tea + Lip Gloss 的全功能 TUI 对话界面
func newChatCmd(configDirRef *string, sandboxModeRef *string, factory ServiceFactory) *cobra.Command {
	var resumeID string
	var continueLatest bool
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
  Ctrl+C     推理中取消本轮 / 空闲时按两次退出
  Ctrl+D     退出程序
  Ctrl+Y     复制最近一次 Agent 回答到系统剪贴板
  Esc        推理中取消本轮 / 空闲时清空输入框
  ↑ / ↓      输入为空时滚动消息历史
  PgUp/PgDn  快速翻页
  鼠标滚轮   滚动消息历史
  Shift+拖选 终端原生选取文本并复制（启用鼠标捕获后需按住 Shift）

内置命令（以 / 开头）:
  /clear     清空当前会话历史，开始新对话
  /copy      复制最近一次 Agent 回答到系统剪贴板
  /copy user 复制最近一次用户消息到系统剪贴板
  /copy all  复制整段对话到系统剪贴板
  /resume ID 恢复指定会话
  /help      显示帮助信息
  /quit      退出程序`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// TUI 模式：禁用 stdout 日志/Trace，避免污染 Bubble Tea 画面
			svc, err := initService(ctx, factory, configDirRef, true, sandboxModeRef)
			if err != nil {
				return fmt.Errorf("初始化失败: %w\n\n请检查配置文件或 API Key 是否正确", err)
			}

			var session *domain.Session
			switch {
			case strings.TrimSpace(resumeID) != "":
				session, err = svc.ResumeSession(ctx, strings.TrimSpace(resumeID), app.SessionScope{AppID: "code"})
			case continueLatest:
				session, err = svc.ContinueSession(ctx, app.SessionScope{AppID: "code"})
			default:
				session, err = svc.CreateSession(ctx, app.SessionScope{AppID: "code"})
			}
			if err != nil {
				return fmt.Errorf("准备会话失败: %w", err)
			}

			// 构建 Bubble Tea 初始 Model
			m := chat.NewModel(ctx, svc, session)
			restoreConsoleOutput := clitui.PrepareConsoleOutput()
			defer restoreConsoleOutput()

			// 启动 TUI 程序
			// WithAltScreen: 使用备用屏幕，退出后恢复原始终端内容。
			// WithMouseCellMotion: 接收滚轮事件以滚动消息区。
			// 原生拖选需按住 Shift（Windows Terminal / 多数现代终端均支持）。
			p := tea.NewProgram(
				m,
				tea.WithAltScreen(),
				tea.WithMouseCellMotion(),
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
	cmd.Flags().StringVar(&resumeID, "resume", "", "恢复指定会话 ID")
	cmd.Flags().BoolVar(&continueLatest, "continue", false, "恢复最近一次会话")
	cmd.MarkFlagsMutuallyExclusive("resume", "continue")

	return cmd
}
