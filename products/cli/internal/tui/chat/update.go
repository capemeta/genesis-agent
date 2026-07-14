package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/runtime/progress"
	approval "genesis-agent/products/cli/internal/approval"
)

// Update 处理所有外部事件，返回新的 Model 状态和待执行的 Cmd
// 遵循 Elm 架构：Message → (Model, Cmd)
// 注意：Model 是值类型，每次 Update 返回新的 Model 副本
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case approval.ApprovalRequiredMsg:
		m.activeApproval = &msg
		m.approvalFocus = true
		m.textinput.Blur()
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, nil

	// ── 终端窗口尺寸变化（初次加载和调整窗口大小时触发）──────
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	// ── 键盘输入 ───────────────────────────────────────────────
	case tea.KeyMsg:
		if m.approvalFocus && m.activeApproval != nil {
			switch strings.ToLower(msg.String()) {
			case "y", "once":
				m.messages = append(m.messages, uiMessage{
					role:    "system",
					content: "🟢 已允许本次操作：" + string(m.activeApproval.Request.Action),
				})
				m.activeApproval.ResultCh <- approvalmodel.Decision{
					Type:   approvalmodel.DecisionApproved,
					Scope:  approvalmodel.GrantScopeOnce,
					Reason: "用户在TUI同意本次操作",
				}
				m.activeApproval = nil
				m.approvalFocus = false
				m.textinput.Focus()
				m.refreshViewportContent()
				m.viewport.GotoBottom()
				return m, nil

			case "s", "session":
				m.messages = append(m.messages, uiMessage{
					role:    "system",
					content: "🟢 已允许当前会话所有此类操作：" + string(m.activeApproval.Request.Action),
				})
				m.activeApproval.ResultCh <- approvalmodel.Decision{
					Type:   approvalmodel.DecisionApprovedForScope,
					Scope:  approvalmodel.GrantScopeSession,
					Reason: "用户在TUI同意当前会话",
				}
				m.activeApproval = nil
				m.approvalFocus = false
				m.textinput.Focus()
				m.refreshViewportContent()
				m.viewport.GotoBottom()
				return m, nil

			case "n", "no", "deny":
				m.messages = append(m.messages, uiMessage{
					role:    "system",
					content: "🔴 已拒绝操作：" + string(m.activeApproval.Request.Action),
				})
				m.activeApproval.ResultCh <- approvalmodel.Decision{
					Type:   approvalmodel.DecisionDenied,
					Scope:  approvalmodel.GrantScopeOnce,
					Reason: "用户在TUI拒绝操作",
				}
				m.activeApproval = nil
				m.approvalFocus = false
				m.textinput.Focus()
				m.refreshViewportContent()
				m.viewport.GotoBottom()
				return m, nil

			case "a", "abort":
				m.messages = append(m.messages, uiMessage{
					role:    "system",
					content: "⚠️ 已终止整个任务",
				})
				m.activeApproval.ResultCh <- approvalmodel.Decision{
					Type:   approvalmodel.DecisionAbort,
					Scope:  approvalmodel.GrantScopeOnce,
					Reason: "用户在TUI终止任务",
				}
				m.activeApproval = nil
				m.approvalFocus = false
				m.textinput.Focus()
				m.refreshViewportContent()
				m.viewport.GotoBottom()
				return m, nil
			}
			return m, nil
		}

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
		m.currentStatus = ""
		m.progressCh = nil
		m.activeApproval = nil
		m.approvalFocus = false
		m.textinput.Focus()
		run := msg.result.Run
		if summary := m.progressSummaryMessage(); summary != "" {
			m.messages = append(m.messages, uiMessage{role: "system", content: summary})
		}
		found := false
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].role == "assistant" {
				// 保留已流式展示的中间思考；仅在缺失时回填 FinalAnswer。
				if run.FinalAnswer != "" && !strings.Contains(m.messages[i].content, run.FinalAnswer) {
					if strings.TrimSpace(m.messages[i].content) == "" {
						m.messages[i].content = run.FinalAnswer
					} else {
						m.messages[i].content = strings.TrimRight(m.messages[i].content, "\n") + "\n\n—— 最终回答 ——\n\n" + run.FinalAnswer
					}
				}
				m.messages[i].steps = len(run.Steps)
				m.messages[i].tokens = run.TotalTokens
				m.messages[i].elapsed = msg.result.Elapsed
				found = true
				break
			}
		}
		if !found {
			m.messages = append(m.messages, uiMessage{
				role:    "assistant",
				content: run.FinalAnswer,
				steps:   len(run.Steps),
				tokens:  run.TotalTokens,
				elapsed: msg.result.Elapsed,
			})
		}
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		// 恢复光标闪烁（推理期间被 spinner tick 替代）
		return m, textinput.Blink

	// ── Agent 推理失败 ─────────────────────────────────────────
	case runErrorMsg:
		m.loading = false
		m.err = msg.err
		m.currentStatus = ""
		m.progressCh = nil
		m.activeApproval = nil
		m.approvalFocus = false
		m.textinput.Focus()
		errContent := "❌ 错误: " + msg.err.Error()
		if summary := m.progressSummaryMessage(); summary != "" {
			m.messages = append(m.messages, uiMessage{role: "system", content: summary})
		}
		m.messages = append(m.messages, uiMessage{
			role:    "system",
			content: errContent,
		})
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, textinput.Blink

	case progressMsg:
		if !m.loading {
			return m, nil
		}
		m.applyProgress(msg.event)
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		cmds = append(cmds, waitProgress(m.progressCh))
	}

	// ── 转发事件给子组件 ────────────────────────────────────────

	// Spinner 仅在加载期间更新（节省不必要的重绘）
	if m.loading {
		var spinCmd tea.Cmd
		m.spinner, spinCmd = m.spinner.Update(msg)
		cmds = append(cmds, spinCmd)
	}

	// Viewport 处理滚动相关事件（↑↓、PgUp/PgDn）
	// 未启用鼠标捕获，以便终端原生拖选复制；滚轮滚动不可用。
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
		// 首次收到窗口尺寸：初始化完成；有历史则 ForUI 水合，否则欢迎语
		m.ready = true
		m.hydrateFromSession()
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
	m.currentStatus = "准备运行 Agent"
	m.progressLog = nil
		m.progressCh = make(chan progressMsg, 256)
	// 更新消息区并滚动到底部
	m.refreshViewportContent()
	m.viewport.GotoBottom()

	// 同时启动：Spinner 动画 + Agent 后台推理
	return m, tea.Batch(
		m.spinner.Tick,                     // 启动加载动画
		waitProgress(m.progressCh),         // 接收结构化运行进度
		m.runAgentCmd(input, m.progressCh), // 后台执行 LLM 推理
	)
}

func (m *Model) applyProgress(event progress.Event) {
	if isLiveAssistantBlock(event.BlockType) {
		m.applyLiveAssistantBlock(event)
		return
	}

	summary := progressSummary(event)
	if summary == "" {
		return
	}
	// Display=false：协议/块闭合事件，不更新状态栏、不写入运行过程摘要。
	if event.Display != nil && !*event.Display {
		return
	}
	m.currentStatus = summary
	if shouldLogProgress(event) {
		// 连续相同摘要去重（例如同工具 start 被重复投递时）
		if n := len(m.progressLog); n > 0 && m.progressLog[n-1] == summary {
			return
		}
		m.progressLog = append(m.progressLog, summary)
	}
}

func isLiveAssistantBlock(blockType string) bool {
	switch blockType {
	case "final_answer", "assistant_draft", "thinking":
		return true
	default:
		return false
	}
}

// applyLiveAssistantBlock 将中间思考 / 最终回答流式写入当前 Turn 的 Agent 气泡。
// assistant_draft/thinking：多轮思考追加；final_answer：在思考之后接最终回答。
func (m *Model) applyLiveAssistantBlock(event progress.Event) {
	// 不可见块不进对话区（协议闭合等）。
	if event.Display != nil && !*event.Display {
		return
	}

	switch event.Phase {
	case progress.PhaseStart:
		idx := m.ensureAssistantMessage()
		if event.BlockType == "final_answer" {
			// 保留中间思考，再接最终回答；无思考时从空开始。
			if strings.TrimSpace(m.messages[idx].content) != "" {
				m.messages[idx].content = strings.TrimRight(m.messages[idx].content, "\n") + "\n\n—— 最终回答 ——\n\n"
			} else {
				m.messages[idx].content = ""
			}
			return
		}
		// 新一轮中间思考：与上一轮之间留空行，避免粘连。
		if strings.TrimSpace(m.messages[idx].content) != "" {
			m.messages[idx].content = strings.TrimRight(m.messages[idx].content, "\n") + "\n\n"
		}
	case progress.PhaseProgress:
		if event.Detail == "" {
			return
		}
		idx := m.ensureAssistantMessage()
		m.messages[idx].content = appendStreamDelta(m.messages[idx].content, event.Detail)
	}
}

func (m *Model) ensureAssistantMessage() int {
	userMsgIdx := -1
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == "user" {
			userMsgIdx = i
			break
		}
	}
	for i := len(m.messages) - 1; i > userMsgIdx; i-- {
		if m.messages[i].role == "assistant" {
			return i
		}
	}
	m.messages = append(m.messages, uiMessage{role: "assistant", content: ""})
	return len(m.messages) - 1
}

// appendStreamDelta 合并流式增量；兼容部分模型返回「累计全文」而非纯 delta 的情况。
func appendStreamDelta(current, delta string) string {
	if delta == "" {
		return current
	}
	if current == "" {
		return delta
	}
	if strings.HasPrefix(delta, current) {
		return delta
	}
	if strings.HasSuffix(current, delta) {
		return current
	}
	// 重叠后缀：current 末尾与 delta 开头有公共部分
	maxOverlap := len(delta)
	if maxOverlap > len(current) {
		maxOverlap = len(current)
	}
	for n := maxOverlap; n > 0; n-- {
		if strings.HasSuffix(current, delta[:n]) {
			return current + delta[n:]
		}
	}
	return current + delta
}

func (m Model) progressSummaryMessage() string {
	if len(m.progressLog) == 0 {
		return ""
	}
	lines := make([]string, 0, len(m.progressLog)+1)
	lines = append(lines, "运行过程:")
	for _, item := range m.progressLog {
		lines = append(lines, "- "+item)
	}
	return strings.Join(lines, "\n")
}

func progressSummary(event progress.Event) string {
	summary := event.Summary
	if summary == "" {
		if event.Name != "" {
			switch event.Kind {
			case progress.KindTool:
				summary = "调用工具: " + event.Name
			case progress.KindSandbox:
				summary = "sandbox: " + event.Name
			case progress.KindLLM:
				summary = "调用 LLM: " + event.Name
			}
		}
	}

	// 针对具体工具参数进行解析并人性化输出
	if event.Kind == progress.KindTool && event.Name != "" {
		detail := event.Detail
		if detail != "" && strings.HasPrefix(detail, "{") {
			if event.Name == "web_search" {
				if event.Phase == progress.PhaseStart && strings.Contains(detail, `"query"`) {
					query := extractJSONField(detail, "query")
					if query != "" {
						return fmt.Sprintf("调用工具: web_search (查询: %s)", truncateString(query, 40))
					}
				} else if event.Phase == progress.PhaseComplete && strings.Contains(detail, `"provider"`) {
					provider := extractJSONField(detail, "provider")
					if provider != "" {
						return fmt.Sprintf("工具执行完成: web_search (搜索引擎: %s)", provider)
					}
				}
			}
			if event.Name == "Skill" && (strings.Contains(detail, `"skill"`) || strings.Contains(detail, `"qualified_name"`) || strings.Contains(detail, `"name"`)) {
				name := extractJSONField(detail, "skill")
				if name == "" {
					name = extractJSONField(detail, "qualified_name")
				}
				if name == "" {
					name = extractJSONField(detail, "name")
				}
				if name != "" {
					if event.Phase == progress.PhaseStart {
						return fmt.Sprintf("加载技能: %s", name)
					}
					return fmt.Sprintf("技能加载完成: %s", name)
				}
			}
			if strings.Contains(detail, `"command"`) {
				cmd := extractJSONField(detail, "command")
				if cmd != "" {
					if event.Phase == progress.PhaseStart {
						return fmt.Sprintf("调用工具: %s (命令: %s)", event.Name, truncateString(cmd, 40))
					}
					return fmt.Sprintf("工具执行完成: %s (命令: %s)", event.Name, truncateString(cmd, 40))
				}
			}
			if strings.Contains(detail, `"path"`) {
				path := extractJSONField(detail, "path")
				if path != "" {
					if event.Phase == progress.PhaseStart {
						return fmt.Sprintf("调用工具: %s (路径: %s)", event.Name, path)
					}
					return fmt.Sprintf("工具执行完成: %s (路径: %s)", event.Name, path)
				}
			}
		}
	}

	return summary
}

func extractJSONField(jsonStr, field string) string {
	idx := strings.Index(jsonStr, `"`+field+`"`)
	if idx == -1 {
		return ""
	}
	sub := jsonStr[idx+len(field)+2:]
	colonIdx := strings.Index(sub, ":")
	if colonIdx == -1 {
		return ""
	}
	sub = sub[colonIdx+1:]
	start := strings.Index(sub, `"`)
	if start == -1 {
		return ""
	}
	sub = sub[start+1:]
	end := strings.Index(sub, `"`)
	if end == -1 {
		return ""
	}
	return sub[:end]
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func shouldLogProgress(event progress.Event) bool {
	if event.Display != nil && !*event.Display {
		return false
	}
	// tool_input 的 complete 与 start 文案重复（非流式无增量），不写入运行过程。
	if event.Kind == progress.KindTool && event.BlockType == "tool_input" && event.Phase == progress.PhaseComplete {
		return false
	}
	switch event.Kind {
	case progress.KindLLM, progress.KindTool, progress.KindSkill, progress.KindSandbox, progress.KindFile:
		return event.Phase == progress.PhaseStart || event.Phase == progress.PhaseComplete || event.Phase == progress.PhaseError
	default:
		return false
	}
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
