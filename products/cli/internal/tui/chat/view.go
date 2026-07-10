package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"genesis-agent/products/cli/internal/tui/styles"
)

// View 渲染整个 TUI 界面
// 从上到下依次：标题栏 → 分隔线 → 消息区 → 状态栏 → 帮助栏 → 输入框
// 注意：各部分行数之和必须等于终端总高度，否则会出现多余空白或截断
func (m Model) View() string {
	if !m.ready {
		// viewport 尚未初始化（未收到 WindowSizeMsg），显示加载提示
		return "\n  正在初始化界面...\n"
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		m.viewport.View(),
		m.statusView(),
		m.helpView(),
		m.inputView(),
	)
}

// headerView 渲染顶部标题栏（2 行：信息行 + 分隔线）
func (m Model) headerView() string {
	// 拼接标题区段（程序名 + 模型名 + 策略 + 会话 ID）
	titlePart := styles.HeaderBar.Render(" Genesis Agent ")
	modelPart := styles.HeaderBarSub.Render(fmt.Sprintf(" %s ", m.modelName()))
	strategyPart := styles.HeaderBarSub.Render(" ReAct Loop ")
	sessionPart := styles.HeaderBarSub.Render(fmt.Sprintf(" %s ", m.shortSessionID()))

	header := lipgloss.JoinHorizontal(lipgloss.Top,
		titlePart, modelPart, strategyPart, sessionPart,
	)

	// 计算已用宽度，用主色填充剩余空间（保证满宽背景）
	usedWidth := lipgloss.Width(header)
	if usedWidth < m.width {
		padding := strings.Repeat(" ", m.width-usedWidth)
		header += styles.HeaderBarFill.Render(padding)
	}

	// 分隔线（第 2 行）
	divider := styles.Divider(m.width)

	return lipgloss.JoinVertical(lipgloss.Left, header, divider)
}

// statusView 渲染状态栏（1 行，固定高度避免布局抖动）
// 加载时：Spinner + 提示文字
// 无状态时：空行（保持布局稳定，不引起跳动）
func (m Model) statusView() string {
	var content string
	switch {
	case m.loading:
		status := m.currentStatus
		if status == "" {
			status = "准备运行 Agent"
		}
		content = styles.StatusLoading.Render(
			fmt.Sprintf("  %s %s", m.spinner.View(), truncateDisplay(status, m.width-8)),
		)
	case m.err != nil:
		// 错误摘要（完整错误已以 system 消息显示在对话区）
		errMsg := m.err.Error()
		if len(errMsg) > m.width-10 {
			errMsg = errMsg[:m.width-13] + "..."
		}
		content = styles.StatusError.Render("  ⚠  " + errMsg)
	}
	// Width 固定为终端宽度，防止不足一行时布局错位
	return lipgloss.NewStyle().Width(m.width).Render(content)
}

func truncateDisplay(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

// helpView 渲染帮助栏（1 行，显示常用快捷键）
func (m Model) helpView() string {
	items := []string{
		styles.HelpKey.Render("Ctrl+C") + " " + styles.HelpBar.Render("退出"),
		styles.HelpKey.Render("/clear") + " " + styles.HelpBar.Render("清空"),
		styles.HelpKey.Render("/help") + " " + styles.HelpBar.Render("帮助"),
		styles.HelpKey.Render("↑↓") + " " + styles.HelpBar.Render("滚动"),
	}
	bar := "  " + strings.Join(items, styles.HelpBar.Render("   "))
	return lipgloss.NewStyle().Width(m.width).Render(bar)
}

// inputView 渲染输入框（3 行：上边框 + 内容行 + 下边框）
// 推理期间显示灰色边框，否则显示主色紫边框
func (m Model) inputView() string {
	borderStyle := styles.InputBorderFocused
	if m.loading {
		// 等待响应时使用灰色边框，暗示禁用状态
		borderStyle = styles.InputBorder
	}
	// Width - 2：减去左右各 1 个边框字符宽度
	return borderStyle.Width(m.width - 2).Render(m.textinput.View())
}

// renderMessages 将 uiMessage 列表渲染为带样式的多行字符串
// 作为 viewport.SetContent() 的输入
func renderMessages(messages []uiMessage, termWidth int) string {
	if len(messages) == 0 {
		return ""
	}

	// 消息内容宽度 = 终端宽度 - 左侧缩进(2) - 右侧边距(2)
	contentWidth := termWidth - 4
	if contentWidth < 10 {
		contentWidth = 10
	}

	var sb strings.Builder

	for _, msg := range messages {
		switch msg.role {
		case "user":
			// 用户消息：蓝色标签 + 蓝色气泡
			sb.WriteString("  ")
			sb.WriteString(styles.UserLabel.Render("你"))
			sb.WriteString("\n")
			sb.WriteString("  ")
			sb.WriteString(styles.UserBubble.Width(contentWidth).Render(msg.content))
			sb.WriteString("\n")

		case "assistant":
			// Agent 回复：紫色标签 + 内容 + 元信息
			sb.WriteString("  ")
			sb.WriteString(styles.AgentLabel.Render("Agent"))
			sb.WriteString("\n")
			sb.WriteString("  ")
			sb.WriteString(styles.AgentBubble.Width(contentWidth).Render(msg.content))
			sb.WriteString("\n")
			// 显示推理元信息（至少有步骤数才显示）
			if msg.steps > 0 || msg.tokens > 0 {
				meta := fmt.Sprintf("  %d 步骤 · %d tokens · %s",
					msg.steps, msg.tokens,
					msg.elapsed.Round(time.Millisecond).String(),
				)
				sb.WriteString("  ")
				sb.WriteString(styles.MetaInfo.Render(meta))
				sb.WriteString("\n")
			}
			sb.WriteString("\n")

		case "system":
			// 系统消息：斜体灰色，用于 /help、错误提示、欢迎语等
			sb.WriteString("  ")
			sb.WriteString(styles.SystemMsg.Width(contentWidth).Render(msg.content))
			sb.WriteString("\n\n")
		}
	}

	return sb.String()
}
