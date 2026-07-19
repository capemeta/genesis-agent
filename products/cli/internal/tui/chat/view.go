package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"genesis-agent/internal/runtime/collab"
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

	sections := []string{
		m.headerView(),
		m.transcriptView(),
		m.statusView(),
		m.helpView(),
	}
	// JoinVertical 会为空字符串仍保留一行。命令菜单关闭时必须完全省略，
	// 否则整帧会比终端多一行，在 Windows cmd 中导致每次重绘都滚屏留下旧帧。
	if commandMenu := m.commandMenuView(); commandMenu != "" {
		sections = append(sections, commandMenu)
	}
	sections = append(sections, m.inputView())
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m Model) commandMenuView() string {
	commands := m.commandMenuCommands()
	if len(commands) == 0 {
		return ""
	}
	const visibleLimit = 5
	start := 0
	if m.commandMenuIndex >= visibleLimit {
		start = m.commandMenuIndex - visibleLimit + 1
	}
	end := start + visibleLimit
	if end > len(commands) {
		end = len(commands)
	}
	lines := make([]string, 0, end-start+1)
	for index := start; index < end; index++ {
		command := commands[index]
		marker := "  "
		if index == m.commandMenuIndex {
			marker = styles.CommandMenuSelected.Render("› ")
		}
		commandWidth := m.width - 3
		if commandWidth < 1 {
			commandWidth = 1
		}
		commandLabel := truncateDisplay(command.value, commandWidth)
		descriptionWidth := m.width - len([]rune(commandLabel)) - 3
		if descriptionWidth < 0 {
			descriptionWidth = 0
		}
		line := marker + styles.CommandMenuCommand.Render(commandLabel) + " " + styles.CommandMenuDescription.Render(truncateDisplay(command.description, descriptionWidth))
		lines = append(lines, line)
	}
	lines = append(lines, styles.CommandMenuHint.Render("↑/↓ 选择  Enter 使用  Esc 关闭"))
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
}

func (m Model) transcriptView() string {
	if !m.helpOverlay {
		return m.viewport.View()
	}
	width := m.width
	if width < 10 {
		width = 10
	}
	height := m.viewport.Height
	if height < minViewport {
		height = minViewport
	}
	return styles.HelpOverlay.Width(width).Height(height).Render(helpText)
}

// headerView 渲染顶部标题栏（2 行：信息行 + 分隔线）
func (m Model) headerView() string {
	// 拼接极简标题区段（左侧品牌 + 右侧状态 chips 与会话 ID）。
	titleLabel := "genesis/chat"
	if m.width > 2 {
		titleLabel = truncateDisplay(titleLabel, m.width-2)
	}
	titlePart := styles.HeaderBar.Render(titleLabel)
	titleWidth := lipgloss.Width(titlePart)
	available := m.width - titleWidth - 2
	metaPart := ""
	if available > 0 {
		parts := []string{
			styles.HeaderChip.Render(truncateDisplay(m.modelName(), 24)),
			styles.HeaderChip.Render(truncateDisplay("模式:"+collab.DisplayName(m.collabMode), 16)),
			styles.HeaderChip.Render(truncateDisplay(m.sandboxLabel(), 18)),
			styles.HeaderBarSub.Render("sess:" + truncateDisplay(m.shortSessionID(), 14)),
		}
		if contextLabel := m.contextUsageLabel(); contextLabel != "" {
			parts = append(parts[:2], append([]string{styles.HeaderChip.Render(contextLabel)}, parts[2:]...)...)
		}
		for len(parts) > 0 {
			candidate := strings.Join(parts, " ")
			if lipgloss.Width(candidate) <= available {
				metaPart = candidate
				break
			}
			parts = parts[:len(parts)-1]
		}
	}

	paddingWidth := m.width - titleWidth - lipgloss.Width(metaPart)
	if paddingWidth < 0 {
		paddingWidth = 0
	}
	header := titlePart + strings.Repeat(" ", paddingWidth) + metaPart
	divider := styles.Divider(m.width)

	return lipgloss.JoinVertical(lipgloss.Left, header, divider)
}

// statusView 渲染状态栏（1 行，固定高度避免布局抖动）
// 加载时：Spinner + 提示文字
// 无状态时：空行（保持布局稳定，不引起跳动）
func (m Model) statusView() string {
	var content string
	switch {
	case m.toast != "":
		content = styles.StatusToast.Render("  " + m.toast)
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
	if ansi.StringWidth(value) <= max {
		return value
	}
	if max <= 3 {
		return ansi.Truncate(value, max, "")
	}
	return ansi.Truncate(value, max, "...")
}

// helpView 渲染帮助栏（1 行，显示常用快捷键，随状态切换）
func (m Model) helpView() string {
	var items []string
	if m.helpOverlay {
		items = []string{styles.HelpKey.Render("Esc") + " " + styles.HelpBar.Render("关闭帮助")}
		return m.renderHelpBar(items)
	}

	if m.loading || m.helpOverlay {
		if m.activeApproval != nil {
			// 审批态
			items = []string{
				styles.HelpKey.Render("Y") + " " + styles.HelpBar.Render("允许本次"),
				styles.HelpKey.Render("S") + " " + styles.HelpBar.Render("会话允许"),
				styles.HelpKey.Render("N") + " " + styles.HelpBar.Render("拒绝"),
				styles.HelpKey.Render("A") + " " + styles.HelpBar.Render("终止任务"),
			}
		} else {
			// 推理中
			items = []string{
				styles.HelpKey.Render("Ctrl+C") + " " + styles.HelpBar.Render("取消推理"),
				styles.HelpKey.Render("↑↓/滚轮") + " " + styles.HelpBar.Render("滚动"),
			}
		}
	} else {
		// 空闲态
		items = []string{
			styles.HelpKey.Render("Ctrl+C") + " " + styles.HelpBar.Render("退出"),
			styles.HelpKey.Render("Ctrl+Y") + " " + styles.HelpBar.Render("复制"),
			styles.HelpKey.Render("v") + " " + styles.HelpBar.Render("选择消息"),
			styles.HelpKey.Render("/help") + " " + styles.HelpBar.Render("帮助"),
			styles.HelpKey.Render("↑↓/滚轮/PgUp/PgDn") + " " + styles.HelpBar.Render("滚动"),
		}
		// 有运行过程记录时显示 o 键提示
		hasProgress := len(m.progressLog) > 0
		if !hasProgress {
			for _, msg := range m.messages {
				if msg.isProgress {
					hasProgress = true
					break
				}
			}
		}
		if hasProgress {
			progressHint := "展开过程"
			if m.progressExpanded {
				progressHint = "折叠过程"
			}
			items = append(items, styles.HelpKey.Render("o")+" "+styles.HelpBar.Render(progressHint))
		}
		// 有活跃计划时显示 p 键提示
		if m.currentPlan != nil {
			planHint := "展开计划"
			if m.planExpanded {
				planHint = "折叠计划"
			}
			items = append(items, styles.HelpKey.Render("p")+" "+styles.HelpBar.Render(planHint))
		}
	}
	if m.selectMode {
		items = []string{
			styles.HelpKey.Render("j/k") + " " + styles.HelpBar.Render("移动"),
			styles.HelpKey.Render("v") + " " + styles.HelpBar.Render("标记起点"),
			styles.HelpKey.Render("y") + " " + styles.HelpBar.Render("复制选择"),
			styles.HelpKey.Render("Esc") + " " + styles.HelpBar.Render("退出选择"),
		}
	}

	return m.renderHelpBar(items)
}

// renderHelpBar 保证帮助栏始终只占一行，窄终端下优先保留左侧高优先级快捷键。
func (m Model) renderHelpBar(items []string) string {
	separator := styles.HelpBar.Render("   ")
	for len(items) > 0 {
		bar := "  " + strings.Join(items, separator)
		if lipgloss.Width(bar) <= m.width {
			return lipgloss.NewStyle().Width(m.width).Render(bar)
		}
		items = items[:len(items)-1]
	}
	return lipgloss.NewStyle().Width(m.width).Render("")
}

// inputView 渲染输入框（上边框 + 内容/textarea + 字数提示 + 下边框）
func (m Model) inputView() string {
	borderStyle := styles.InputBorderFocused
	if m.loading {
		// 等待响应时使用灰色边框，暗示禁用状态
		borderStyle = styles.InputBorder
	}

	contentWidth := m.width - 4 // border (2) + padding (2)
	if contentWidth < 10 {
		contentWidth = 10
	}

	// 拼接 textarea 视图和右对齐的字数指示行
	length := m.textarea.Length()
	limit := m.textarea.CharLimit
	countStr := fmt.Sprintf("%d/%d", length, limit)
	completionHint := "输入 / 显示命令"
	if strings.HasPrefix(strings.TrimSpace(m.textarea.Value()), "/") {
		completionHint = "↑/↓ 选择命令"
	}
	hintWidth := contentWidth - lipgloss.Width(countStr) - lipgloss.Width(completionHint) - 1
	if hintWidth < 0 {
		hintWidth = 0
	}
	countLine := completionHint + strings.Repeat(" ", hintWidth) + " " + countStr
	countLineStyled := lipgloss.NewStyle().Foreground(styles.ColorDarkGray).Render(countLine)

	combined := lipgloss.JoinVertical(lipgloss.Left,
		m.textarea.View(),
		countLineStyled,
	)

	return borderStyle.Width(m.width - 2).Render(combined)
}

// renderMessages 将 uiMessage 列表渲染为带样式的多行字符串
// 作为 viewport.SetContent() 的输入
func renderMessages(messages []uiMessage, termWidth int, progressExpanded, selectMode bool, selectAnchor, selectCursor int) string {
	if len(messages) == 0 {
		return ""
	}

	// 消息内容宽度 = 终端宽度 - 左侧缩进(2) - 右侧边距(2)
	contentWidth := termWidth - 4
	if contentWidth < 10 {
		contentWidth = 10
	}

	var sb strings.Builder

	selectableIndexes := selectableIndexes(messages)
	for index, msg := range messages {
		switch msg.role {
		case "user":
			// 用户消息：蓝色标签 + 平铺正文（无大面积背景色）
			sb.WriteString("  ")
			sb.WriteString(renderSelectionMarker(index, selectableIndexes, selectMode, selectAnchor, selectCursor))
			sb.WriteString(styles.UserLabel.Render("你"))
			sb.WriteString("\n")
			sb.WriteString("  ")
			sb.WriteString(styles.UserBubble.Width(contentWidth).Render(msg.content))
			sb.WriteString("\n")

		case "assistant":
			// Agent 回复：紫色标签 + 内容 + 元信息
			sb.WriteString("  ")
			sb.WriteString(renderSelectionMarker(index, selectableIndexes, selectMode, selectAnchor, selectCursor))
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
			// 系统消息：如果是运行过程日志，支持折叠/展开渲染
			if msg.isProgress {
				var content string
				if progressExpanded {
					lines := []string{"▾ " + activitySummary(msg) + " · [o] 折叠:"}
					for _, item := range msg.progressLog {
						lines = append(lines, "  - "+item)
					}
					content = strings.Join(lines, "\n")
				} else {
					content = "▸ " + activitySummary(msg) + " · [o] 展开"
				}
				sb.WriteString("  ")
				sb.WriteString(styles.SystemMsg.Width(contentWidth).Render(content))
				sb.WriteString("\n\n")
			} else {
				// 普通系统消息：斜体灰色，用于 /help、错误提示、欢迎语等
				sb.WriteString("  ")
				sb.WriteString(styles.SystemMsg.Width(contentWidth).Render(msg.content))
				sb.WriteString("\n\n")
			}
		}
	}

	return sb.String()
}

func activitySummary(msg uiMessage) string {
	outcome := msg.activityOutcome
	if outcome == "" {
		outcome = "完成"
	}
	elapsed := msg.activityElapsed.Round(time.Millisecond)
	if msg.activityTokens <= 0 {
		if elapsed <= 0 {
			return fmt.Sprintf("运行过程（%d 步 · %s）", len(msg.progressLog), outcome)
		}
		return fmt.Sprintf("运行过程（%d 步 · %s · %s）", len(msg.progressLog), elapsed, outcome)
	}
	if elapsed <= 0 {
		return fmt.Sprintf("运行过程（%d 步 · %d tokens · %s）", len(msg.progressLog), msg.activityTokens, outcome)
	}
	return fmt.Sprintf("运行过程（%d 步 · %d tokens · %s · %s）", len(msg.progressLog), msg.activityTokens, elapsed, outcome)
}

func selectableIndexes(messages []uiMessage) []int {
	indexes := make([]int, 0, len(messages))
	for index, message := range messages {
		if message.role == "user" || message.role == "assistant" {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func renderSelectionMarker(messageIndex int, selectable []int, selectMode bool, anchor, cursor int) string {
	if !selectMode {
		return ""
	}
	selectedIndex := -1
	for index, candidate := range selectable {
		if candidate == messageIndex {
			selectedIndex = index
			break
		}
	}
	if selectedIndex < 0 {
		return "  "
	}
	if selectedIndex == cursor {
		return styles.SelectionCursor.Render("▶ ")
	}
	if anchor >= 0 && ((anchor <= cursor && selectedIndex >= anchor && selectedIndex <= cursor) || (anchor > cursor && selectedIndex >= cursor && selectedIndex <= anchor)) {
		return styles.SelectionRange.Render("• ")
	}
	return "  "
}
