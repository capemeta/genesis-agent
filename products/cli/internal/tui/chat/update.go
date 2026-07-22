package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"genesis-agent/internal/app"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/collab"
	"genesis-agent/internal/runtime/progress"
	approval "genesis-agent/products/cli/internal/approval"
	localcollab "genesis-agent/shared/local/collab"
)

// Update 处理所有外部事件，返回新的 Model 状态和待执行的 Cmd
// 遵循 Elm 架构：Message → (Model, Cmd)
// 注意：Model 是值类型，每次 Update 返回新的 Model 副本
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case approval.ApprovalRequiredMsg:
		m.enqueueApproval(msg)
		m.textarea.Blur()
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, nil

	// ── 终端窗口尺寸变化（初次加载和调整窗口大小时触发）──────
	case clearToastMsg:
		// 定时清除 toast
		if time.Now().After(m.toastExpiresAt) {
			m.toast = ""
		}
		return m, nil

	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	// ── 鼠标事件（滚轮滚动 + 应用内拖选复制）──────────────────
	case tea.MouseMsg:
		return m.handleMouse(msg)

	// ── 键盘输入 ───────────────────────────────────────────────
	case tea.KeyMsg:
		if m.approvalFocus && m.activeApproval != nil {
			choices := approval.BuildChoices(m.activeApproval.Request, m.activeApproval.Policy)
			if choice, ok := approval.MatchChoice(choices, msg.String()); ok {
				if choice.Decision.Type == approvalmodel.DecisionAbort {
					m.messages = append(m.messages, uiMessage{
						role:    "system",
						content: "⚠️ 已终止整个任务",
					})
					// cancelCurrentRound 会向当前与排队审批投递 Abort，并取消 Run。
					m = m.cancelCurrentRound()
					m.refreshViewportContent()
					m.viewport.GotoBottom()
					return m, nil
				}
				prefix := "🟢 "
				switch choice.Decision.Type {
				case approvalmodel.DecisionDenied:
					prefix = "🔴 "
				}
				m.messages = append(m.messages, uiMessage{
					role:    "system",
					content: prefix + choice.Label + "：" + string(m.activeApproval.Request.Action),
				})
				m.resolveActiveApproval(choice.Decision)
				if m.activeApproval == nil {
					m.textarea.Focus()
				} else {
					m.textarea.Blur()
				}
				m.refreshViewportContent()
				m.viewport.GotoBottom()
				return m, nil
			}
			return m, nil
		}

		if m.helpOverlay {
			switch strings.ToLower(msg.String()) {
			case "esc", "?":
				return m.closeHelpOverlay(), nil
			}
			if msg.Type != tea.KeyCtrlC && msg.Type != tea.KeyCtrlD {
				return m, nil
			}
		}

		if m.selectMode {
			switch strings.ToLower(msg.String()) {
			case "ctrl+d":
				m.cancelFn()
				return m, tea.Quit
			case "j", "down":
				return m.moveSelection(1), nil
			case "k", "up":
				return m.moveSelection(-1), nil
			case "v":
				m.selectAnchor = m.selectCursor
				m.refreshViewportContent()
				return m, nil
			case "y":
				return m.copySelection()
			case "esc":
				return m.exitSelectMode(), nil
			}
			if msg.Type != tea.KeyCtrlC && msg.Type != tea.KeyCtrlD {
				return m, nil
			}
		}

		switch msg.Type {

		case tea.KeyCtrlC:
			if m.loading {
				// 推理中：取消本轮，不退出程序
				m = m.cancelCurrentRound()
				return m.showToast("已取消本轮推理")
			}
			// 空闲态：二次确认退出
			now := time.Now()
			if m.ctrlCLastAt != nil && now.Sub(*m.ctrlCLastAt) < time.Second {
				m.cancelFn()
				return m, tea.Quit
			}
			m.ctrlCLastAt = &now
			return m.showToast("再按一次退出，或输入 /quit")

		case tea.KeyCtrlD:
			m.cancelFn()
			return m, tea.Quit

		case tea.KeyEsc:
			if m.commandMenuOpen {
				m.commandMenuOpen = false
				m.commandMenuQuery = ""
				m.viewport.Height = m.viewportHeight()
				return m, nil
			}
			if m.loading {
				// 推理态：取消本轮（不退出）
				m = m.cancelCurrentRound()
				return m.showToast("已取消本轮推理")
			}
			// 空闲态：清空输入框
			m.textarea.Reset()
			m.historyIdx = -1
			return m, nil

		case tea.KeyCtrlY:
			// 复制最近一条 Agent 回答到系统剪贴板
			return m.copyLastAssistant()

		case tea.KeyCtrlP:
			// Ctrl+P/N 专用于输入历史，避免与消息区滚动争抢方向键。
			if !m.loading && len(m.history) > 0 {
				if m.historyIdx == -1 {
					m.tempInput = m.textarea.Value()
					m.historyIdx = len(m.history) - 1
				} else if m.historyIdx > 0 {
					m.historyIdx--
				}
				m.textarea.SetValue(m.history[m.historyIdx])
				m.textarea.CursorEnd()
				return m, nil
			}

		case tea.KeyCtrlN:
			if !m.loading && m.historyIdx != -1 {
				m.historyIdx++
				if m.historyIdx >= len(m.history) {
					m.historyIdx = -1
					m.textarea.SetValue(m.tempInput)
				} else {
					m.textarea.SetValue(m.history[m.historyIdx])
				}
				m.textarea.CursorEnd()
				return m, nil
			}

		case tea.KeyTab:
			if m.commandMenuOpen {
				m = m.applyCommandMenuSelection()
				return m, nil
			}
			if !m.loading && strings.HasPrefix(strings.TrimSpace(m.textarea.Value()), "/") {
				m = m.completeSlashCommand()
				return m, nil
			}

		case tea.KeyUp:
			if m.commandMenuOpen {
				return m.moveCommandMenu(-1), nil
			}
			if m.loading {
				m.viewport.LineUp(1)
				return m, nil
			}
			// Composer 为空时，方向键按界面提示滚动消息区。
			// 输入历史改用 Ctrl+P/N，不再让「↑」看起来失效。
			if m.textarea.Value() == "" && m.historyIdx == -1 {
				m.viewport.LineUp(1)
				return m, nil
			}

		case tea.KeyDown:
			if m.commandMenuOpen {
				return m.moveCommandMenu(1), nil
			}
			if m.loading {
				m.viewport.LineDown(1)
				return m, nil
			}
			if m.textarea.Value() == "" && m.historyIdx == -1 {
				m.viewport.LineDown(1)
				return m, nil
			}

		case tea.KeyPgUp:
			// 翻页始终作用于消息区，不依赖 Composer 是否为空。
			m.viewport.PageUp()
			return m, nil

		case tea.KeyPgDown:
			m.viewport.PageDown()
			return m, nil

		case tea.KeyEnter:
			if m.commandMenuOpen && msg.String() != "shift+enter" && msg.String() != "alt+enter" {
				commands := m.commandMenuCommands()
				input := strings.TrimSpace(m.textarea.Value())
				if len(commands) == 0 || commands[m.commandMenuIndex].value != input {
					m = m.applyCommandMenuSelection()
					return m, nil
				}
				// 已输入完整命令时，Enter 应直接执行，不再停留在“使用候选”。
				m.commandMenuOpen = false
				m.commandMenuQuery = ""
				m.viewport.Height = m.viewportHeight()
			}
			if msg.String() == "shift+enter" || msg.String() == "alt+enter" {
				m.textarea.InsertString("\n")
				return m, nil
			}
			// 等待 Agent 响应期间忽略新的发送请求
			if m.loading {
				return m, nil
			}
			input := strings.TrimSpace(m.textarea.Value())
			if input == "" {
				return m, nil
			}
			m.textarea.Reset()

			// 保存至输入历史
			m.history = append(m.history, input)
			m.historyIdx = -1
			m.tempInput = ""

			if strings.HasPrefix(input, "#记住") {
				return m.handleRememberCmd(strings.TrimPrefix(input, "#记住"))
			}
			if strings.HasPrefix(input, "#remember") {
				return m.handleRememberCmd(strings.TrimPrefix(input, "#remember"))
			}
			// 斜杠命令（/clear、/help 等）
			if strings.HasPrefix(input, "/") {
				return m.handleSlashCmd(input)
			}
			// 发送消息给 Agent
			return m.sendMessage(input)

		default:
			if msg.String() == "y" && (!m.textarea.Focused() || m.historyIdx != -1) {
				return m.copyLastAssistant()
			}
			if msg.String() == "v" && !m.loading && strings.TrimSpace(m.textarea.Value()) == "" {
				return m.enterSelectMode()
			}
			if msg.String() == "?" && !m.loading && strings.TrimSpace(m.textarea.Value()) == "" {
				return m.showHelpOverlay()
			}
			// o 键：展开/折叠运行过程日志；加载期间输入框已禁用，仍允许控制视图。
			if msg.String() == "o" {
				if strings.TrimSpace(m.textarea.Value()) == "" {
					m.progressExpanded = !m.progressExpanded
					m.refreshViewportContent()
					// 展开后内容变长，滚到底才能看全；折叠则保持当前位置。
					if m.progressExpanded {
						m.viewport.GotoBottom()
					}
					return m, nil
				}
			}
			// p 键：展开/折叠计划卡片（仅 Composer 输入为空且非加载时生效）
			if msg.String() == "p" && m.currentPlan != nil && !m.loading {
				if strings.TrimSpace(m.textarea.Value()) == "" {
					m.planExpanded = !m.planExpanded
					m.refreshViewportContent()
					if m.planExpanded {
						m.viewport.GotoBottom()
					}
					return m, nil
				}
			}
		}

	// ── Agent 推理成功完成 ──────────────────────────────────────
	case runCompleteMsg:
		followTail := m.viewport.AtBottom()
		m.loading = false
		m.err = nil
		m.currentStatus = ""
		m.progressCh = nil
		m.rejectAllPendingApprovals(approvalmodel.Decision{
			Type:   approvalmodel.DecisionDenied,
			Scope:  approvalmodel.GrantScopeOnce,
			Reason: "run completed",
		})
		m.textarea.Focus()
		run := msg.result.Run
		if len(m.progressLog) > 0 {
			logCopy := make([]string, len(m.progressLog))
			copy(logCopy, m.progressLog)
			m.messages = append(m.messages, uiMessage{
				role:            "system",
				isProgress:      true,
				progressLog:     logCopy,
				activityTokens:  run.TotalTokens,
				activityElapsed: msg.result.Elapsed,
				activityOutcome: "完成",
			})
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
		if followTail {
			m.viewport.GotoBottom()
		}
		// 恢复光标闪烁（推理期间被 spinner tick 替代）
		return m, textarea.Blink

	// ── Agent 推理失败 ─────────────────────────────────────────
	case runErrorMsg:
		if msg.err.Error() == "操作已取消" {
			return m, nil
		}
		followTail := m.viewport.AtBottom()
		m.loading = false
		m.err = msg.err
		m.currentStatus = ""
		m.progressCh = nil
		m.rejectAllPendingApprovals(approvalmodel.Decision{
			Type:   approvalmodel.DecisionDenied,
			Scope:  approvalmodel.GrantScopeOnce,
			Reason: "run error",
		})
		m.textarea.Focus()
		errContent := "❌ 错误: " + msg.err.Error()
		if len(m.progressLog) > 0 {
			logCopy := make([]string, len(m.progressLog))
			copy(logCopy, m.progressLog)
			m.messages = append(m.messages, uiMessage{
				role:            "system",
				isProgress:      true,
				progressLog:     logCopy,
				activityElapsed: time.Since(m.runStartedAt),
				activityOutcome: "失败",
			})
		}
		m.messages = append(m.messages, uiMessage{
			role:    "system",
			content: errContent,
		})
		m.refreshViewportContent()
		if followTail {
			m.viewport.GotoBottom()
		}
		return m, textarea.Blink

	case progressMsg:
		if !m.loading {
			return m, nil
		}
		m.applyProgress(msg.event)
		now := time.Now()
		if m.lastProgressRenderAt.IsZero() || now.Sub(m.lastProgressRenderAt) >= progressRenderInterval {
			followTail := m.viewport.AtBottom()
			m.refreshViewportContent()
			if followTail {
				m.viewport.GotoBottom()
			}
			m.lastProgressRenderAt = now
			m.progressDirty = false
		} else {
			m.progressDirty = true
			cmds = append(cmds, flushProgressAfter(progressRenderInterval-now.Sub(m.lastProgressRenderAt)))
		}
		cmds = append(cmds, waitProgress(m.progressCh))

	case flushProgressMsg:
		if m.loading && m.progressDirty {
			followTail := m.viewport.AtBottom()
			m.refreshViewportContent()
			if followTail {
				m.viewport.GotoBottom()
			}
			m.lastProgressRenderAt = time.Now()
			m.progressDirty = false
		}
	}

	// ── 转发事件给子组件 ────────────────────────────────────────

	// Spinner 仅在加载期间更新（节省不必要的重绘）
	if m.loading {
		var spinCmd tea.Cmd
		m.spinner, spinCmd = m.spinner.Update(msg)
		cmds = append(cmds, spinCmd)
	}

	// Viewport 处理翻页键。空输入时的↑/↓已在上方显式处理。
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	// Textarea 在非加载时接收按键（加载时禁用防止误操作）
	if !m.loading {
		var taCmd tea.Cmd
		m.textarea, taCmd = m.textarea.Update(msg)
		cmds = append(cmds, taCmd)
		if key, ok := msg.(tea.KeyMsg); ok && key.Type != tea.KeyTab {
			m.commandSuggestionIndex = -1
			m.commandSuggestionPrefix = ""
		}
		m.syncCommandMenu()
	}

	return m, tea.Batch(cmds...)
}

// handleWindowSize 处理终端窗口大小变化事件
func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	// Windows 终端与 Lip Gloss 对 CJK、符号和中英文混排的显示宽度可能存在偏差。
	// 只保留 1 列时，渲染器认为未满行，但终端可能已经物理换行并滚屏，旧帧便会形成重影。
	// 固定预留多列安全区，任何应用内容都不接近终端自动换行边界。
	m.width = msg.Width - rightEdgeSafetyColumns
	if m.width < 1 {
		m.width = 1
	}
	m.height = msg.Height

	// 更新 viewport 尺寸
	m.viewport.Width = m.width
	m.viewport.Height = m.viewportHeight()

	// 更新输入框宽度（减去圆角边框 2 + 内边距 2）
	inputWidth := m.width - 4
	if inputWidth < 10 {
		inputWidth = 10
	}
	m.textarea.SetWidth(inputWidth)

	if !m.ready {
		// 首次收到窗口尺寸：初始化完成；有历史则 ForUI 水合，否则欢迎语
		m.ready = true
		m.hydrateFromSession()
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, textarea.Blink
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
	m.textarea.Reset()
	// 进入加载状态
	m.loading = true
	m.err = nil
	m.currentStatus = "准备运行 Agent"
	m.progressLog = nil
	m.progressCallIDs = nil
	m.progressDirty = false
	m.lastProgressRenderAt = time.Time{}
	m.runStartedAt = time.Now()
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

	// 处理计划事件：完整 Plan JSON 快照，直接替换 currentPlan
	if event.Kind == progress.KindTaskList {
		if event.Detail != "" {
			var plan domain.TaskList
			if err := json.Unmarshal([]byte(event.Detail), &plan); err == nil {
				m.currentPlan = &plan
				// 新建计划时自动展开；更新/tick 时保持当前展开状态
				if event.BlockType == "create" || event.BlockType == "hydrate" {
					m.planExpanded = true
				}
			}
		}
		return // 计划事件不写入 progressLog、不更新 status bar
	}
	if event.Kind == progress.KindCollaborationMode {
		m.collabMode = collab.Normalize(collab.Mode(event.Detail))
		if event.Summary != "" {
			m.toast = event.Summary
			m.toastExpiresAt = time.Now().Add(3 * time.Second)
		}
		return
	}
	if event.Kind == progress.KindPlanDocument {
		if event.Summary != "" {
			m.toast = event.Summary
			m.toastExpiresAt = time.Now().Add(3 * time.Second)
		}
		return
	}

	if isLiveAssistantBlock(event.BlockType) {
		if event.Depth > 0 || event.SubAgentID != "" {
			// 子 Agent 的 Live Assistant Block (thinking/draft)：
			// 不污染主对话气泡，而是转化为进度摘要原位呈现在 progressLog 的思考行中！
			if (event.BlockType == "thinking" || event.BlockType == "assistant_draft") && strings.TrimSpace(event.Detail) != "" {
				subName := event.SubAgentID
				if subName == "" {
					subName = "Worker"
				}
				prefix := fmt.Sprintf("[Sub-Agent: %s] 思考: ", subName)
				cleanDetail := strings.ReplaceAll(event.Detail, "\n", " ")
				thinkingIdx := -1
				nLogs := len(m.progressLog)
				if nLogs > 0 {
					lastLog := m.progressLog[nLogs-1]
					if strings.HasPrefix(lastLog, prefix) || (strings.HasPrefix(lastLog, "[Sub-Agent:") && strings.Contains(lastLog, "] 思考:")) {
						thinkingIdx = nLogs - 1
					}
				}
				if thinkingIdx != -1 {
					currText := strings.TrimPrefix(m.progressLog[thinkingIdx], prefix)
					fullText := currText + cleanDetail
					m.progressLog[thinkingIdx] = prefix + fullText
				} else {
					newThinking := prefix + cleanDetail
					m.progressLog = append(m.progressLog, newThinking)
					m.progressCallIDs = append(m.progressCallIDs, "")
				}
			}
			return
		}
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

		// 如果有 CallID，则尝试原位替换该工具调用的最新执行进度
		replaced := false
		if event.CallID != "" {
			nLogs := len(m.progressLog)
			for i, cid := range m.progressCallIDs {
				if cid == event.CallID {
					// 仅当该 CallID 处于 progressLog 的最末尾（中途无思考或其他工具插队）时原位更新
					if i == nLogs-1 {
						oldLog := m.progressLog[i]
						// 🟢 保留子智能体 Tag 前缀
						if strings.HasPrefix(oldLog, "[Sub-Agent:") && !strings.HasPrefix(summary, "[Sub-Agent:") {
							idx := strings.Index(oldLog, "] ")
							if idx != -1 {
								summary = oldLog[:idx+2] + summary
							}
						}
						m.progressLog[i] = summary
						replaced = true
					} else {
						// 若中途已有思考/其他工具日志插队，清空旧 CallID 避免错乱，改为在最底部按时间线追加
						m.progressCallIDs[i] = ""
					}
					break
				}
			}
		}
		if !replaced {
			m.progressLog = append(m.progressLog, summary)
			m.progressCallIDs = append(m.progressCallIDs, event.CallID)
		}
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
			if strings.TrimSpace(m.messages[idx].content) != "" {
				m.messages[idx].content = strings.TrimRight(m.messages[idx].content, "\n") + "\n\n—— 最终回答 ——\n\n"
			} else {
				m.messages[idx].content = ""
			}
			return
		}
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
				summary = "Thinking / 正在思考推理中..."
			}
		} else if event.Kind == progress.KindLLM {
			summary = "Thinking / 正在思考推理中..."
		}
	}

	detail := strings.TrimSpace(event.Detail)
	if event.Kind == progress.KindTool {
		if event.Name == "run_skill_command" && (event.Phase == progress.PhaseComplete || event.Phase == progress.PhaseError) {
			label := "工具执行完成: run_skill_command"
			if event.Phase == progress.PhaseError {
				label = "工具执行失败: run_skill_command"
			}
			backendTag := ""
			if backend, ok := event.Metadata["selected_backend"]; ok && backend != "" {
				switch backend {
				case "remote_sandbox":
					backendTag = "[远程容器沙箱]"
				case "local_platform_sandbox":
					backendTag = "[本地平台沙箱]"
				case "local_host":
					backendTag = "[宿主直跑]"
				default:
					backendTag = "[" + backend + "]"
				}
			}
			if backendTag != "" {
				label = label + " " + backendTag
			}
			parts := make([]string, 0, 2)
			if cmd := extractJSONField(detail, "command"); cmd != "" {
				parts = append(parts, "命令: "+truncateString(cmd, 40))
			}
			if timing := formatSkillTiming(event.Metadata); timing != "" {
				parts = append(parts, timing)
			}
			if len(parts) > 0 {
				summary = label + " (" + strings.Join(parts, "；") + ")"
			} else {
				summary = label
			}
		} else if detail != "" && strings.HasPrefix(detail, "{") {
			if event.Name == "web_search" {
				if event.Phase == progress.PhaseStart && strings.Contains(detail, `"query"`) {
					query := extractJSONField(detail, "query")
					if query != "" {
						summary = fmt.Sprintf("调用工具: web_search (查询: %s)", truncateString(query, 40))
					}
				} else if event.Phase == progress.PhaseComplete && strings.Contains(detail, `"provider"`) {
					provider := extractJSONField(detail, "provider")
					if provider != "" {
						summary = fmt.Sprintf("工具执行完成: web_search (搜索引擎: %s)", provider)
					}
				}
			} else if event.Name == "Skill" && (strings.Contains(detail, `"skill"`) || strings.Contains(detail, `"qualified_name"`) || strings.Contains(detail, `"name"`)) {
				name := extractJSONField(detail, "skill")
				if name == "" {
					name = extractJSONField(detail, "qualified_name")
				}
				if name == "" {
					name = extractJSONField(detail, "name")
				}
				if name != "" {
					if event.Phase == progress.PhaseStart {
						summary = fmt.Sprintf("加载技能: %s", name)
					} else {
						summary = fmt.Sprintf("技能加载完成: %s", name)
					}
				}
			} else if strings.Contains(detail, `"command"`) {
				cmd := extractJSONField(detail, "command")
				if cmd != "" {
					if event.Phase == progress.PhaseStart {
						summary = fmt.Sprintf("调用工具: %s (命令: %s)", event.Name, truncateString(cmd, 40))
					} else {
						summary = fmt.Sprintf("工具执行完成: %s (命令: %s)", event.Name, truncateString(cmd, 40))
					}
				}
			} else if strings.Contains(detail, `"path"`) {
				path := extractJSONField(detail, "path")
				if path != "" {
					if event.Phase == progress.PhaseStart {
						summary = fmt.Sprintf("调用工具: %s (路径: %s)", event.Name, path)
					} else {
						summary = fmt.Sprintf("工具执行完成: %s (路径: %s)", event.Name, path)
					}
				}
			}
		}
	}

	if event.Depth > 0 || event.SubAgentID != "" {
		subName := event.SubAgentID
		if subName == "" {
			subName = "Worker"
		}
		if !strings.HasPrefix(summary, "[Sub-Agent") {
			summary = fmt.Sprintf("[Sub-Agent: %s] %s", subName, summary)
		}
	}

	return summary
}

func formatSkillTiming(metadata map[string]string) string {
	if len(metadata) == 0 {
		return ""
	}
	labels := []struct {
		key   string
		label string
	}{
		{key: "duration_ms", label: "总计"},
		{key: "approval_duration_ms", label: "审批"},
		{key: "staging_duration_ms", label: "staging"},
		{key: "execution_duration_ms", label: "执行"},
	}
	parts := make([]string, 0, len(labels))
	for _, item := range labels {
		value, err := strconv.ParseInt(metadata[item.key], 10, 64)
		if err != nil || value < 0 {
			continue
		}
		parts = append(parts, item.label+" "+formatMilliseconds(value))
	}
	return strings.Join(parts, "｜")
}

func formatMilliseconds(value int64) string {
	if value < 1000 {
		return fmt.Sprintf("%dms", value)
	}
	return fmt.Sprintf("%.1fs", float64(value)/1000)
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
	m.textarea.Reset()
	m.commandMenuOpen = false
	m.commandMenuQuery = ""
	m.viewport.Height = m.viewportHeight()

	trimmed := strings.TrimSpace(strings.TrimPrefix(input, "/"))
	parts := strings.SplitN(trimmed, " ", 2)
	cmdName := strings.ToLower(parts[0])
	cmdArg := ""
	if len(parts) > 1 {
		cmdArg = parts[1]
	}

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
		m.currentPlan = nil
		m.planExpanded = false
		// 显示确认消息
		m.messages = append(m.messages, uiMessage{
			role:    "system",
			content: "✅ 会话历史已清空，开始新对话。",
		})
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, nil

	case "resume":
		if m.loading {
			return m.showToast("当前推理尚未结束")
		}
		sessionID := strings.TrimSpace(cmdArg)
		scope := m.currentSessionScope()
		var session *domain.Session
		var err error
		if sessionID == "" {
			session, err = m.svc.ContinueSession(context.Background(), scope)
		} else {
			session, err = m.svc.ResumeSession(context.Background(), sessionID, scope)
		}
		if err != nil {
			m.messages = append(m.messages, uiMessage{role: "system", content: "恢复会话失败: " + err.Error()})
			m.refreshViewportContent()
			m.viewport.GotoBottom()
			return m, nil
		}
		m.session = session
		m.err = nil
		m.progressLog = nil
		m.progressCallIDs = nil
		m.progressExpanded = true
		m.hydrateFromSession()
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, nil

	case "sessions":
		sessions, err := m.svc.ListSessions(context.Background(), m.currentSessionScope(), 10)
		if err != nil {
			m.messages = append(m.messages, uiMessage{role: "system", content: "加载会话列表失败: " + err.Error()})
		} else if len(sessions) == 0 {
			m.messages = append(m.messages, uiMessage{role: "system", content: "没有可恢复的历史会话。"})
		} else {
			var lines []string
			for _, session := range sessions {
				title := strings.TrimSpace(session.Title)
				if title == "" {
					title = "未命名会话"
				}
				lines = append(lines, fmt.Sprintf("%s  %s", session.ID, title))
			}
			m.messages = append(m.messages, uiMessage{role: "system", content: "最近会话:\n" + strings.Join(lines, "\n")})
		}
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, nil

	case "plan":
		return m.handlePlanSlash(cmdArg)

	case "execute":
		return m.handleExecuteSlash()

	case "help", "?", "h":
		return m.showHelpOverlay()

	case "remember", "记住":
		return m.handleRememberCmd(cmdArg)

	case "copy":
		arg := strings.ToLower(strings.TrimSpace(cmdArg))
		switch arg {
		case "user":
			return m.copyLastUser()
		case "all":
			return m.copyAllTranscript()
		default:
			// 默认复制最近的一条 Agent 回答 (assistant 消息)
			return m.copyLastAssistant()
		}

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

func (m Model) handlePlanSlash(arg string) (tea.Model, tea.Cmd) {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if m.collabStore == nil || m.session == nil {
		return m.systemNotice("协作模式存储未就绪，无法切换规划模式。")
	}
	ctx := context.Background()
	sessionID := m.session.ID
	switch arg {
	case "cancel", "off", "exit":
		if err := m.collabStore.Set(ctx, sessionID, collab.SessionState{Mode: collab.ModeDefault, HandoffPending: false}); err != nil {
			return m.systemNotice("退出规划模式失败: " + err.Error())
		}
		m.collabMode = collab.ModeDefault
		return m.systemNotice("已放弃规划，回到执行中（未注入任务清单交接）。")
	case "", "on", "enter":
		if m.collabMode == collab.ModePlan {
			return m.systemNotice("已处于规划模式。完成后用 /execute 批准退出，或 /plan cancel 放弃。")
		}
		if err := m.collabStore.Set(ctx, sessionID, collab.SessionState{Mode: collab.ModePlan, HandoffPending: false}); err != nil {
			return m.systemNotice("进入规划模式失败: " + err.Error())
		}
		m.collabMode = collab.ModePlan
		return m.systemNotice("已进入规划模式：只读调研 + 写实施方案；禁用任务清单与变更类工具。")
	default:
		return m.systemNotice("用法: /plan | /plan cancel")
	}
}

func (m Model) handleExecuteSlash() (tea.Model, tea.Cmd) {
	if m.collabStore == nil || m.session == nil {
		return m.systemNotice("协作模式存储未就绪。")
	}
	if m.collabMode != collab.ModePlan {
		return m.systemNotice("当前不在规划模式。请先 /plan，或让 Agent 调用 enter_plan_mode。")
	}
	rel, body, err := localcollab.ReadPlanDocument(m.workspaceRoot, m.session.ID)
	if err != nil {
		return m.systemNotice("读取实施方案失败: " + err.Error())
	}
	if strings.TrimSpace(body) == "" {
		return m.systemNotice(fmt.Sprintf("尚未写入实施方案（期望路径 %s）。请先让 Agent 调用 write_implementation_plan。", rel))
	}
	ctx := context.Background()
	if err := m.collabStore.Set(ctx, m.session.ID, collab.SessionState{Mode: collab.ModeDefault, HandoffPending: true}); err != nil {
		return m.systemNotice("批准退出失败: " + err.Error())
	}
	m.collabMode = collab.ModeDefault
	return m.systemNotice(fmt.Sprintf(
		"已批准实施方案并退出规划模式（%s）。下一条消息将交接：先读方案 → todo_write → 再执行。",
		rel,
	))
}

func (m Model) systemNotice(text string) (tea.Model, tea.Cmd) {
	m.messages = append(m.messages, uiMessage{role: "system", content: text})
	m.refreshViewportContent()
	m.viewport.GotoBottom()
	return m, nil
}

func (m Model) currentSessionScope() app.SessionScope {
	if m.session == nil {
		return app.SessionScope{AppID: "code"}
	}
	return app.SessionScope{
		TenantID: m.session.TenantID,
		UserID:   m.session.UserID,
		AgentID:  m.session.AgentID,
		AppID:    m.session.AppID,
	}
}

// handleRememberCmd 处理用户主动沉淀长期记忆的指令
func (m Model) handleRememberCmd(content string) (tea.Model, tea.Cmd) {
	// 清空输入框
	m.textarea.Reset()

	content = strings.TrimSpace(content)
	if content == "" {
		m.messages = append(m.messages, uiMessage{
			role:    "system",
			content: "⚠️ 记忆内容不能为空。示例: #记住 数据库连接首选 PostgreSQL",
		})
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, nil
	}

	userID := m.session.UserID
	if userID == "" {
		userID = "user"
	}
	tenantID := m.session.TenantID
	if tenantID == "" {
		tenantID = "dev"
	}

	err := m.svc.SaveLongTermMemory(context.Background(), tenantID, userID, content)
	if err != nil {
		m.messages = append(m.messages, uiMessage{
			role:    "system",
			content: "❌ 保存长期记忆失败: " + err.Error(),
		})
	} else {
		m.messages = append(m.messages, uiMessage{
			role:    "system",
			content: "🟢 已为您记牢：“" + content + "”。后续对话启动时将自适应召回该偏好参考。",
		})
	}

	m.refreshViewportContent()
	m.viewport.GotoBottom()
	return m, nil
}
