package chat

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"genesis-agent/internal/app"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/progress"
	approval "genesis-agent/products/cli/internal/approval"
	"genesis-agent/products/cli/internal/tui/styles"
)

// 固定 UI 元素占用行数
// 注意：各行数需与 View() 中的实际渲染保持一致
const (
	headerLines = 2 // 标题行（1）+ 分隔线（1）
	footerLines = 2 // 状态栏（1）+ 帮助栏（1）
	inputLines  = 3 // 上边框（1）+ 内容（1）+ 下边框（1）
	minViewport = 5 // 消息区最小可用行数
)

// Model 是 Bubble Tea 的核心状态机
// 实现 tea.Model 接口：Init() / Update() / View()
type Model struct {
	// ── Bubble Tea 组件 ────────────────────────────────────────
	viewport  viewport.Model  // 可滚动消息历史区
	textinput textinput.Model // 单行文本输入框
	spinner   spinner.Model   // 等待时的加载动画

	// ── 运行时依赖（构造函数注入）────────────────────────────
	ctx      context.Context    // 携带取消信号的上下文
	cancelFn context.CancelFunc // 用于 Ctrl+C 时取消正在执行的推理
	svc      app.AgentService   // 应用服务（封装 engine / memory 细节）
	session  *domain.Session    // 当前对话会话

	// ── 对话状态 ──────────────────────────────────────────────
	messages      []uiMessage      // 当前会话所有消息（用于 View 渲染）
	loading       bool             // 是否正在等待 Agent 推理响应
	err           error            // 最近一次错误（nil 表示无错误）
	progressCh    chan progressMsg // 当前 run 的进度事件通道
	currentStatus string           // 状态栏当前展示文本
	progressLog   []string         // 最近的过程摘要，独立于最终回答

	// ── 布局状态 ──────────────────────────────────────────────
	width  int  // 当前终端宽度（通过 WindowSizeMsg 更新）
	height int  // 当前终端高度（通过 WindowSizeMsg 更新）
	ready  bool // viewport 是否已完成首次初始化

	// ── 审批流状态 ────────────────────────────────────────────
	activeApproval *approval.ApprovalRequiredMsg // 当前等待人工确认的审批请求
	approvalFocus  bool                          // 当前是否为审批输入拦截状态
}

// NewModel 创建 TUI 对话界面 Model（Bubble Tea 入口）
// 接入层只依赖 AgentService 接口，不感知 engine / memory 等领域细节
func NewModel(
	ctx context.Context,
	svc app.AgentService,
	session *domain.Session,
) Model {
	// 创建可取消的子上下文，用于 Ctrl+C 时取消正在执行的 LLM 请求
	ctx, cancelFn := context.WithCancel(ctx)

	// 配置文本输入框
	ti := textinput.New()
	ti.Placeholder = "输入消息... (Enter 发送 | /help 查看命令 | Ctrl+C 退出)"
	ti.CharLimit = 2000
	ti.Focus()
	ti.PromptStyle = lipgloss.NewStyle().
		Foreground(styles.ColorPrimary).
		Bold(true)
	ti.TextStyle = lipgloss.NewStyle().
		Foreground(styles.ColorWhite)

	// 配置加载动画
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(styles.ColorYellow)

	// Viewport 初始尺寸为 0，收到 WindowSizeMsg 后更新
	vp := viewport.New(0, minViewport)
	vp.SetContent("")

	return Model{
		ctx:       ctx,
		cancelFn:  cancelFn,
		svc:       svc,
		session:   session,
		viewport:  vp,
		textinput: ti,
		spinner:   s,
	}
}

// hydrateFromSession 用短期记忆完整链经 ForUI 投影水合聊天气泡。
// 本轮运行中的进度条仍由 progress 事件驱动；水合只负责历史/恢复场景。
func (m *Model) hydrateFromSession() {
	m.messages = nil
	if m.svc == nil || m.session == nil || m.session.ID == "" {
		m.messages = append(m.messages, welcomeMsg(m.modelName(), m.shortSessionID()))
		return
	}
	hist, err := m.svc.ListSessionMessages(m.ctx, m.session.ID)
	if err != nil {
		m.messages = append(m.messages, uiMessage{
			role:    "system",
			content: "加载会话历史失败：" + err.Error(),
		})
		m.messages = append(m.messages, welcomeMsg(m.modelName(), m.shortSessionID()))
		return
	}
	if len(hist) == 0 {
		m.messages = append(m.messages, welcomeMsg(m.modelName(), m.shortSessionID()))
		return
	}
	m.messages = projectUIMessages(hist)
	if len(m.messages) == 0 {
		m.messages = append(m.messages, welcomeMsg(m.modelName(), m.shortSessionID()))
	}
}

// Init 返回 Bubble Tea 初始化命令（启动光标闪烁）
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// viewportHeight 计算消息区可用行数（总高度减去所有固定 UI 元素）
func (m Model) viewportHeight() int {
	h := m.height - headerLines - footerLines - inputLines
	if h < minViewport {
		return minViewport
	}
	return h
}

// refreshViewportContent 重新渲染消息列表并更新 viewport 内容
// 在每次 messages 变化后调用
func (m *Model) refreshViewportContent() {
	msgs := m.messages
	if m.loading && (len(m.progressLog) > 0 || m.activeApproval != nil) {
		tempMsgs := make([]uiMessage, len(m.messages)+1)
		copy(tempMsgs, m.messages)
		if m.activeApproval != nil {
			tempMsgs[len(m.messages)] = uiMessage{
				role:    "system",
				content: m.renderApprovalCard(),
			}
		} else {
			tempMsgs[len(m.messages)] = uiMessage{
				role:    "system",
				content: m.progressSummaryMessage(),
			}
		}
		msgs = tempMsgs
	}
	m.viewport.SetContent(renderMessages(msgs, m.width))
}

func (m *Model) renderApprovalCard() string {
	if m.activeApproval == nil {
		return ""
	}
	req := m.activeApproval.Request

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorYellow).
		Padding(1, 2).
		Width(m.width - 4)

	titleStyle := lipgloss.NewStyle().
		Foreground(styles.ColorYellow).
		Bold(true)

	boldStyle := lipgloss.NewStyle().Bold(true)

	display := req.Resource.Display
	if display == "" {
		display = req.Resource.URI
	}

	content := fmt.Sprintf(
		"%s\n\n"+
			"  %s: %s\n"+
			"  %s: %s\n"+
			"  %s: %s\n"+
			"  %s: %s\n\n"+
			"%s",
		titleStyle.Render("🛡️ 需要授权的操作 (Human Approval Required)"),
		boldStyle.Render("工具"), req.ToolName,
		boldStyle.Render("动作"), req.Action,
		boldStyle.Render("资源"), display,
		boldStyle.Render("原因"), req.Reason,
		lipgloss.NewStyle().Foreground(styles.ColorGreen).Bold(true).
			Render("按键授权: [Y]允许本次 / [S]允许当前会话 / [N]拒绝 / [A]终止任务"),
	)

	return borderStyle.Render(content)
}

func waitProgress(ch <-chan progressMsg) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// runAgentCmd 构造一个 Bubble Tea Cmd，在后台 goroutine 中执行 Agent 推理
// 推理结果通过 runCompleteMsg 或 runErrorMsg 发回 Update 循环
func (m Model) runAgentCmd(input string, progressCh chan<- progressMsg) tea.Cmd {
	return func() tea.Msg {
		if progressCh != nil {
			defer close(progressCh)
		}
		emit := func(event progress.Event) {
			if progressCh == nil {
				return
			}
			// 必须可靠投递：丢弃 PhaseStart 会导致 final_answer 无法清空，对话区出现段落叠加/重复。
			select {
			case progressCh <- progressMsg{event: event}:
			case <-m.ctx.Done():
			}
		}
		result, err := m.svc.RunOnce(m.ctx, app.RunRequest{
			SessionID:  m.session.ID,
			TenantID:   m.session.TenantID,
			Input:      input,
			OnProgress: emit,
		})

		if err != nil {
			// 区分上下文取消（用户主动退出）和真实错误
			if m.ctx.Err() != nil {
				return runErrorMsg{err: fmt.Errorf("操作已取消")}
			}
			return runErrorMsg{err: err}
		}
		return runCompleteMsg{result: result}
	}
}

// modelName 返回当前 LLM 模型名（用于标题栏显示）
func (m Model) modelName() string {
	if agent := m.svc.DefaultAgent(); agent != nil && agent.DefaultModel != "" {
		return agent.DefaultModel
	}
	if cfg := m.svc.Config(); cfg != nil {
		if resolved, err := cfg.LLM.ResolveRoute("chat"); err == nil {
			return resolved.Model
		}
	}
	return "unknown"
}

// shortSessionID 返回截短的会话 ID（避免标题栏过长）
func (m Model) shortSessionID() string {
	if len(m.session.ID) > 22 {
		return m.session.ID[:22] + "…"
	}
	return m.session.ID
}

// helpText /help 命令显示的帮助内容
const helpText = `可用命令（以 / 开头）:
  /clear   清空当前会话历史，开始新对话
  /help    显示此帮助
  /quit    退出程序

快捷键:
  Enter    发送消息
  Esc      清空输入框
  Ctrl+C   退出程序
  ↑ / ↓    滚动消息历史（一行）
  PgUp     向上翻页
  PgDn     向下翻页`

// welcomeMsg 首次进入对话时显示的欢迎消息
func welcomeMsg(modelName, sessionID string) uiMessage {
	return uiMessage{
		role: "system",
		content: fmt.Sprintf(
			"欢迎使用 Genesis Agent！\n模型: %s  |  会话: %s\n\n直接输入消息开始对话，输入 /help 查看可用命令。",
			modelName, sessionID,
		),
	}
}
