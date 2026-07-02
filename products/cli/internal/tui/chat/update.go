package chat

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Update 处理所有外部事件，返回新的 Model 状态和待执行的 Cmd
// 遵循 Elm 架构：Message → (Model, Cmd)
// 注意：Model 是值类型，每次 Update 返回新的 Model 副本
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── 终端窗口尺寸变化（初次加载和调整窗口大小时触发）──────
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	// ── 键盘输入 ───────────────────────────────────────────────
	case tea.KeyMsg:
		switch msg.Type {

		case tea.KeyCtrlC:
			// 取消正在执行的 LLM 请求，然后退出程序
			m.cancelFn()
			return m, tea.Quit

		case tea.KeyEsc:
			// 清空输入框（不退出）
			m.textinput.SetValue("")
			return m, nil

		case tea.KeyEnter:
			// 等待 Agent 响应期间忽略新的发送请求
			if m.loading {
				return m, nil
			}
			input := strings.TrimSpace(m.textinput.Value())
			if input == "" {
				return m, nil
			}
			// 斜杠命令（/clear、/help 等）
			if strings.HasPrefix(input, "/") {
				return m.handleSlashCmd(input)
			}
			// 发送消息给 Agent
			return m.sendMessage(input)
		}

	// ── Agent 推理成功完成 ──────────────────────────────────────
	case runCompleteMsg:
		m.loading = false
		m.err = nil
		run := msg.result.Run
		m.messages = append(m.messages, uiMessage{
			role:    "assistant",
			content: run.FinalAnswer,
			steps:   len(run.Steps),
			tokens:  run.TotalTokens,
			elapsed: msg.result.Elapsed,
		})
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		// 恢复光标闪烁（推理期间被 spinner tick 替代）
		return m, textinput.Blink

	// ── Agent 推理失败 ─────────────────────────────────────────
	case runErrorMsg:
		m.loading = false
		m.err = msg.err
		errContent := "❌ 错误: " + msg.err.Error()
		m.messages = append(m.messages, uiMessage{
			role:    "system",
			content: errContent,
		})
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, textinput.Blink
	}

	// ── 转发事件给子组件 ────────────────────────────────────────

	// Spinner 仅在加载期间更新（节省不必要的重绘）
	if m.loading {
		var spinCmd tea.Cmd
		m.spinner, spinCmd = m.spinner.Update(msg)
		cmds = append(cmds, spinCmd)
	}

	// Viewport 处理滚动相关事件（↑↓、PgUp/PgDn、鼠标滚轮）
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	// TextInput 在非加载时接收按键（加载时禁用防止误操作）
	if !m.loading {
		var tiCmd tea.Cmd
		m.textinput, tiCmd = m.textinput.Update(msg)
		cmds = append(cmds, tiCmd)
	}

	return m, tea.Batch(cmds...)
}

// handleWindowSize 处理终端窗口大小变化事件
func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height

	// 更新 viewport 尺寸
	m.viewport.Width = msg.Width
	m.viewport.Height = m.viewportHeight()

	// 更新输入框宽度（减去圆角边框 2 + 内边距 2）
	inputWidth := msg.Width - 4
	if inputWidth < 10 {
		inputWidth = 10
	}
	m.textinput.Width = inputWidth

	if !m.ready {
		// 首次收到窗口尺寸：初始化完成，显示欢迎消息
		m.ready = true
		m.messages = append(m.messages, welcomeMsg(m.modelName(), m.shortSessionID()))
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, textinput.Blink
	}

	// 后续 resize：重新渲染消息（宽度可能变化，需要重新换行）
	m.refreshViewportContent()
	return m, nil
}

// sendMessage 处理用户发送消息的完整流程
func (m Model) sendMessage(input string) (tea.Model, tea.Cmd) {
	// 添加用户消息到历史
	m.messages = append(m.messages, uiMessage{role: "user", content: input})
	// 清空输入框
	m.textinput.SetValue("")
	// 进入加载状态
	m.loading = true
	m.err = nil
	// 更新消息区并滚动到底部
	m.refreshViewportContent()
	m.viewport.GotoBottom()

	// 同时启动：Spinner 动画 + Agent 后台推理
	return m, tea.Batch(
		m.spinner.Tick,       // 启动加载动画
		m.runAgentCmd(input), // 后台执行 LLM 推理
	)
}

// handleSlashCmd 处理 / 开头的斜杠命令
func (m Model) handleSlashCmd(input string) (tea.Model, tea.Cmd) {
	// 清空输入框
	m.textinput.SetValue("")

	// 提取命令名称（去掉 /，转小写，去空格）
	cmdName := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(input, "/")))

	switch cmdName {
	case "quit", "exit", "q":
		m.cancelFn()
		return m, tea.Quit

	case "clear", "cls":
		// 通过 AgentService 清空会话历史（解耦 memory 细节）
		_ = m.svc.ClearSession(context.Background(), m.session.ID)
		// 清空 UI 消息列表
		m.messages = nil
		m.err = nil
		// 显示确认消息
		m.messages = append(m.messages, uiMessage{
			role:    "system",
			content: "✅ 会话历史已清空，开始新对话。",
		})
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, nil

	case "help", "?", "h":
		m.messages = append(m.messages, uiMessage{
			role:    "system",
			content: helpText,
		})
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, nil

	default:
		m.messages = append(m.messages, uiMessage{
			role:    "system",
			content: "❓ 未知命令: " + input + "\n  输入 /help 查看可用命令",
		})
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, nil
	}
}
