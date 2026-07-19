package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"genesis-agent/internal/app"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/collab"
	"genesis-agent/internal/runtime/progress"
	approval "genesis-agent/products/cli/internal/approval"
	"genesis-agent/products/cli/internal/tui/clipboard"
	"genesis-agent/products/cli/internal/tui/styles"
)

// 固定 UI 元素占用行数
// 注意：各行数需与 View() 中的实际渲染保持一致
const (
	headerLines            = 2                      // 标题行（1）+ 分隔线（1）
	footerLines            = 2                      // 状态栏（1）+ 帮助栏（1）
	inputLines             = 6                      // 上边框（1）+ 内容/textarea（3）+ 字数（1）+ 下边框（1）
	minViewport            = 5                      // 消息区最小可用行数
	rightEdgeSafetyColumns = 4                      // Windows 对中英文混排宽度估算存在偏差，预留安全列避免物理换行
	progressRenderInterval = 125 * time.Millisecond // 最多 8Hz 刷新进度区
)

// Model 是 Bubble Tea 的核心状态机
// 实现 tea.Model 接口：Init() / Update() / View()
type Model struct {
	// ── Bubble Tea 组件 ────────────────────────────────────────
	viewport viewport.Model // 可滚动消息历史区
	textarea textarea.Model // 多行文本输入框
	spinner  spinner.Model  // 等待时的加载动画

	// ── 运行时依赖（构造函数注入）────────────────────────────
	ctx      context.Context    // 携带取消信号的上下文
	cancelFn context.CancelFunc // 用于 Ctrl+C 时取消正在执行的推理
	svc      app.AgentService   // 应用服务（封装 engine / memory 细节）
	session  *domain.Session    // 当前对话会话

	// ── 对话状态 ──────────────────────────────────────────────
	messages             []uiMessage      // 当前会话所有消息（用于 View 渲染）
	loading              bool             // 是否正在等待 Agent 推理响应
	err                  error            // 最近一次错误（nil 表示无错误）
	progressCh           chan progressMsg // 当前 run 的进度事件通道
	currentStatus        string           // 状态栏当前展示文本
	progressLog          []string         // 最近的过程摘要，独立于最终回答
	progressCallIDs      []string         // 运行过程进度 CallID 缓存，用于按 CallID 原位替换
	progressExpanded     bool             // 运行过程日志是否展开（o 键切换）
	progressDirty        bool             // 有尚未绘制的进度更新
	lastProgressRenderAt time.Time        // 最近一次进度区重绘时刻
	runStartedAt         time.Time        // 当前轮次开始时间，用于失败摘要

	// ── 布局状态 ──────────────────────────────────────────────
	width  int  // 当前终端宽度（通过 WindowSizeMsg 更新）
	height int  // 当前终端高度（通过 WindowSizeMsg 更新）
	ready  bool // viewport 是否已完成首次初始化

	// ── 审批流状态 ────────────────────────────────────────────
	activeApproval *approval.ApprovalRequiredMsg // 当前等待人工确认的审批请求
	approvalFocus  bool                          // 当前是否为审批输入拦截状态

	// ── 计划状态 ──────────────────────────────────────────────
	currentPlan  *domain.TaskList // 当前活跃任务清单（nil 表示无清单）
	planExpanded bool             // 任务清单卡片是否展开

	// ── 协作模式（规划模式）────────────────────────────────────
	collabStore   collab.Store
	workspaceRoot string
	collabMode    collab.Mode

	// ── Toast 状态 ────────────────────────────────────────────
	toast          string    // 短时提示文本（空则不显示）
	toastExpiresAt time.Time // toast 过期时间

	// ── Ctrl+C 状态 ─────────────────────────────────────────
	parentCtx   context.Context // 根 context（不被 cancelFn 取消，重建子 ctx 时使用）
	ctrlCLastAt *time.Time      // 空闲态首次按下 Ctrl+C 的时刻（用于二次确认退出）

	// ── 输入历史 ──────────────────────────────────────────────
	history    []string // 用户已发送输入历史
	historyIdx int      // 当前历史索引，-1 表示未处于浏览态
	tempInput  string   // 浏览前暂存的草稿输入

	// ── Phase 3 交互增强 ───────────────────────────────────────
	selectMode   bool // 消息级选择模式
	selectAnchor int  // 选择起点；-1 表示尚未标记
	selectCursor int  // 当前选择光标，对应 selectableMessageIndexes 的下标
	helpOverlay  bool // 快捷键与命令帮助覆盖层

	commandSuggestionIndex  int    // Tab 循环补全当前候选下标；-1 表示未激活
	commandSuggestionPrefix string // 本轮补全的初始前缀，保证 Tab 能循环候选
	commandMenuOpen         bool   // / 命令选择菜单是否展示
	commandMenuIndex        int    // 菜单中当前高亮项
	commandMenuQuery        string // 打开菜单时的查询文本
}

// NewModel 创建 TUI 对话界面 Model（Bubble Tea 入口）
// 接入层只依赖 AgentService 接口，不感知 engine / memory 等领域细节
// WithCollab 注入协作模式依赖（规划模式切换）；可链式调用。
func (m Model) WithCollab(store collab.Store, workspaceRoot string) Model {
	m.collabStore = store
	m.workspaceRoot = workspaceRoot
	if m.workspaceRoot == "" {
		m.workspaceRoot = "."
	}
	m.collabMode = m.loadCollabMode()
	return m
}

func (m Model) loadCollabMode() collab.Mode {
	if m.collabStore == nil || m.session == nil || m.session.ID == "" {
		return collab.ModeDefault
	}
	st, err := m.collabStore.Get(context.Background(), m.session.ID)
	if err != nil {
		return collab.ModeDefault
	}
	return collab.Normalize(st.Mode)
}

func NewModel(
	ctx context.Context,
	svc app.AgentService,
	session *domain.Session,
) Model {
	// 保留根 context，取消后重建可取消子 context 时需要
	parentCtx := ctx
	// 创建可取消的子上下文，用于 Ctrl+C 时取消正在执行的 LLM 请求
	ctx, cancelFn := context.WithCancel(parentCtx)

	// 配置多行文本输入框
	ta := textarea.New()
	ta.Placeholder = "输入消息... (Enter 发送 | Shift+Enter/Ctrl+J 换行 | Ctrl+Y 复制)"
	ta.CharLimit = 4000
	ta.SetHeight(3) // 默认高度 3 行
	ta.ShowLineNumbers = false
	ta.Focus()
	ta.Prompt = "" // 不加前导字符，使其更像现代聊天输入框

	// 配置样式
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(styles.ColorGray)
	ta.FocusedStyle.Text = lipgloss.NewStyle().Foreground(styles.ColorWhite)

	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(styles.ColorDarkGray)
	ta.BlurredStyle.Text = lipgloss.NewStyle().Foreground(styles.ColorGray)

	// 配置加载动画
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(styles.ColorYellow)

	// Viewport 初始尺寸为 0，收到 WindowSizeMsg 后更新。
	// 禁用 pager 字母键（b/f/j/k/u/d/空格），避免与 Composer 输入冲突；
	// ↑↓ 由 Update 显式处理，PgUp/PgDn 与鼠标滚轮交给 viewport。
	vp := viewport.New(0, minViewport)
	vp.KeyMap = viewport.KeyMap{
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdn", "page down"),
		),
		HalfPageUp:   key.NewBinding(key.WithDisabled()),
		HalfPageDown: key.NewBinding(key.WithDisabled()),
		Up:           key.NewBinding(key.WithDisabled()),
		Down:         key.NewBinding(key.WithDisabled()),
		Left:         key.NewBinding(key.WithDisabled()),
		Right:        key.NewBinding(key.WithDisabled()),
	}
	vp.SetContent("")

	return Model{
		parentCtx:              parentCtx,
		ctx:                    ctx,
		cancelFn:               cancelFn,
		svc:                    svc,
		session:                session,
		viewport:               vp,
		textarea:               ta,
		spinner:                s,
		historyIdx:             -1, // 初始化为未浏览历史状态
		selectAnchor:           -1,
		commandSuggestionIndex: -1,
	}
}

// hydrateFromSession 用短期记忆完整链经 ForUI 投影水合聊天气泡。
// 本轮运行中的进度条仍由 progress 事件驱动；水合只负责历史/恢复场景。
func (m *Model) hydrateFromSession() {
	m.messages = nil
	m.currentPlan = nil
	m.planExpanded = false
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
	// 从消息历史中恢复计划状态（取最后一条 plan_snapshot）
	m.currentPlan = loadPlanFromMessages(hist)
	if m.currentPlan != nil {
		m.planExpanded = true // 恢复后默认展开，让用户直观看到当前进度
	}
	m.collabMode = m.loadCollabMode()
}

// Init 返回 Bubble Tea 初始化命令（启动光标闪烁）
func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

// viewportHeight 计算消息区可用行数（总高度减去所有固定 UI 元素）
func (m Model) viewportHeight() int {
	h := m.height - headerLines - footerLines - inputLines - m.commandMenuHeight()
	if h < minViewport {
		return minViewport
	}
	return h
}

// refreshViewportContent 重新渲染消息列表并更新 viewport 内容
// 在每次 messages 变化后调用
func (m *Model) refreshViewportContent() {
	msgs := m.messages

	// 收集加载期间的附加系统消息（进度/审批/计划）
	var extraMsgs []uiMessage
	if m.loading {
		if m.activeApproval != nil {
			extraMsgs = append(extraMsgs, uiMessage{
				role:    "system",
				content: m.renderApprovalCard(),
			})
		} else if len(m.progressLog) > 0 {
			extraMsgs = append(extraMsgs, uiMessage{
				role:            "system",
				isProgress:      true,
				progressLog:     m.progressLog,
				activityOutcome: "进行中",
			})
		}
	}

	// 计划卡片：加载中或空闲时只要有活跃计划就持续显示
	if m.currentPlan != nil {
		extraMsgs = append(extraMsgs, uiMessage{
			role:    "system",
			content: m.renderPlanCard(),
		})
	}

	if len(extraMsgs) > 0 {
		tempMsgs := make([]uiMessage, len(m.messages)+len(extraMsgs))
		copy(tempMsgs, m.messages)
		copy(tempMsgs[len(m.messages):], extraMsgs)
		msgs = tempMsgs
	}

	m.viewport.SetContent(renderMessages(msgs, m.width, m.progressExpanded, m.selectMode, m.selectAnchor, m.selectCursor))
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

// renderPlanCard 渲染当前活跃计划的 TUI 卡片
// 样式：蓝色圆角边框 + 进度条 + 条目列表（✓绿/▶黄/☐灰）
func (m *Model) renderPlanCard() string {
	if m.currentPlan == nil {
		return ""
	}
	p := m.currentPlan
	contentWidth := m.width - 8 // 边框(2) + padding(4) + margin(2)
	if contentWidth < 20 {
		contentWidth = 20
	}

	// 折叠模式：只显示一行摘要
	if !m.planExpanded {
		done := p.DoneCount()
		total := p.TotalCount()
		summary := fmt.Sprintf("📋 %s  %s  %d/%d · [p] 展开",
			p.Title,
			styles.ProgressBar(p.ProgressPct(), 10),
			done, total,
		)
		return styles.PlanBorder.Width(m.width - 4).Render(
			styles.PlanTitle.Render(summary),
		)
	}

	// 展开模式：完整卡片
	var sb strings.Builder

	// 标题行
	titleLine := fmt.Sprintf("📋 %s  (v%d)", p.Title, p.Version)
	sb.WriteString(styles.PlanTitle.Render(titleLine))
	sb.WriteString("\n")

	// 进度行
	pct := p.ProgressPct()
	progressLine := fmt.Sprintf("%s  %d/%d (%d%%)",
		styles.ProgressBar(pct, 20),
		p.DoneCount(), p.TotalCount(), pct,
	)
	sb.WriteString(styles.PlanProgressBar.Render(progressLine))
	sb.WriteString("\n")

	if p.Summary != "" {
		sb.WriteString(styles.PlanHint.Render(p.Summary))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// 条目列表
	for _, item := range p.Items {
		var icon, text string
		switch item.Status {
		case domain.TaskListItemDone:
			icon = styles.TaskListItemDone.Render("✓")
			text = styles.TaskListItemDone.Render(item.Text)
			if item.Note != "" {
				text += styles.PlanHint.Render(" (" + item.Note + ")")
			}
		case domain.TaskListItemDoing:
			icon = styles.TaskListItemDoing.Render("▶")
			text = styles.TaskListItemDoing.Render(item.Text)
		case domain.TaskListItemFailed:
			icon = styles.TaskListItemFailed.Render("✗")
			text = styles.TaskListItemFailed.Render(item.Text)
			if item.Note != "" {
				text += styles.PlanHint.Render(" (" + item.Note + ")")
			}
		case domain.TaskListItemSkipped:
			icon = styles.TaskListItemPending.Render("-")
			text = styles.TaskListItemPending.Render(item.Text)
		default: // pending
			icon = styles.TaskListItemPending.Render("☐")
			text = item.Text
		}
		sb.WriteString(fmt.Sprintf("  %s  %s\n", icon, text))
	}

	// 底部提示
	sb.WriteString("\n")
	hint := "[p] 折叠"
	if p.IsCompleted() {
		hint = "✓ 全部完成  " + hint
	}
	sb.WriteString(styles.PlanHint.Render(hint))

	return styles.PlanBorder.Width(m.width - 4).Render(sb.String())
}

// loadPlanFromMessages 从消息列表中找最后一条 plan_snapshot 恢复计划
func loadPlanFromMessages(msgs []*domain.Message) *domain.TaskList {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Kind == domain.MessageKindTaskListSnapshot && msgs[i].Content != "" {
			var plan domain.TaskList
			if err := json.Unmarshal([]byte(msgs[i].Content), &plan); err == nil {
				return &plan
			}
		}
	}
	return nil
}

// ── Toast 辅助 ────────────────────────────────────────────────

// showToast 显示一条短时提示（2 秒后自动清除）并返回清除 Cmd。
func (m Model) showToast(text string) (tea.Model, tea.Cmd) {
	m.toast = text
	m.toastExpiresAt = time.Now().Add(2 * time.Second)
	m.refreshViewportContent()
	return m, clearToastAfter(2 * time.Second)
}

// clearToastAfter 在 d 时间后发出 clearToastMsg，由 Update 清除 toast。
func clearToastAfter(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return clearToastMsg{}
	}
}

// ── 中断辅助 ─────────────────────────────────────────────────

// cancelCurrentRound 取消当前推理轮次：
// 调用 cancelFn 终止进行中的 LLM 请求，然后重建可取消子 context，
// 恢复为 Idle 态（不退出程序）。
func (m Model) cancelCurrentRound() Model {
	m.cancelFn()
	ctx, cancelFn := context.WithCancel(m.parentCtx)
	m.ctx = ctx
	m.cancelFn = cancelFn
	m.loading = false
	m.currentStatus = ""
	m.progressCh = nil
	m.activeApproval = nil
	m.approvalFocus = false
	m.textarea.Focus()
	return m
}

// ── 复制辅助 ─────────────────────────────────────────────────

// copyLastAssistant 复制最近一条 assistant 消息到系统剪贴板。
func (m Model) copyLastAssistant() (tea.Model, tea.Cmd) {
	text, ok := m.lastMessageContent("assistant")
	if !ok {
		return m.showToast("暂无可复制的回答")
	}
	if err := clipboard.Write(text); err != nil {
		return m.showCopyFailureToast(err)
	}
	return m.showToast("✓ 已复制到剪贴板")
}

// copyLastUser 复制最近一条 user 消息到系统剪贴板。
func (m Model) copyLastUser() (tea.Model, tea.Cmd) {
	text, ok := m.lastMessageContent("user")
	if !ok {
		return m.showToast("暂无可复制的用户消息")
	}
	if err := clipboard.Write(text); err != nil {
		return m.showCopyFailureToast(err)
	}
	return m.showToast("✓ 已复制用户消息")
}

func (m Model) lastMessageContent(role string) (string, bool) {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == role {
			text := strings.TrimSpace(m.messages[i].content)
			if text == "" {
				continue
			}
			return text, true
		}
	}
	return "", false
}

// copyAllTranscript 复制全部可见 transcript（纯文本，去掉 ANSI 样式）。
func (m Model) copyAllTranscript() (tea.Model, tea.Cmd) {
	if len(m.messages) == 0 {
		return m.showToast("对话为空")
	}
	var sb strings.Builder
	for _, msg := range m.messages {
		if msg.role == "system" {
			continue // 跳过内部 system 消息（进度/审批/计划）
		}
		prefix := "你"
		if msg.role == "assistant" {
			prefix = "Agent"
		}
		sb.WriteString(prefix)
		sb.WriteString(": ")
		sb.WriteString(strings.TrimSpace(msg.content))
		sb.WriteString("\n\n")
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return m.showToast("对话为空")
	}
	if err := clipboard.Write(text); err != nil {
		return m.showCopyFailureToast(err)
	}
	return m.showToast(fmt.Sprintf("✓ 已复制全部对话（%d 条消息）", len(m.messages)))
}

func (m Model) showCopyFailureToast(err error) (tea.Model, tea.Cmd) {
	return m.showToast("复制失败: " + err.Error() + "；可输入 /copy 重试或检查终端环境")
}

// selectableMessageIndexes 返回可在选择模式中复制的用户和 Agent 消息。
func (m Model) selectableMessageIndexes() []int {
	indexes := make([]int, 0, len(m.messages))
	for i, msg := range m.messages {
		if msg.role == "user" || msg.role == "assistant" {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func (m Model) enterSelectMode() (tea.Model, tea.Cmd) {
	indexes := m.selectableMessageIndexes()
	if len(indexes) == 0 {
		return m.showToast("暂无可选择的消息")
	}
	m.selectMode = true
	m.selectCursor = len(indexes) - 1
	m.selectAnchor = m.selectCursor
	m.textarea.Blur()
	m.refreshViewportContent()
	return m, nil
}

func (m Model) exitSelectMode() Model {
	m.selectMode = false
	m.selectAnchor = -1
	m.commandSuggestionIndex = -1
	m.commandSuggestionPrefix = ""
	m.textarea.Focus()
	m.refreshViewportContent()
	return m
}

func (m Model) showHelpOverlay() (tea.Model, tea.Cmd) {
	m.helpOverlay = true
	m.textarea.Blur()
	return m, nil
}

func (m Model) closeHelpOverlay() Model {
	m.helpOverlay = false
	m.textarea.Focus()
	return m
}

func (m Model) moveSelection(delta int) Model {
	indexes := m.selectableMessageIndexes()
	if len(indexes) == 0 {
		return m
	}
	m.selectCursor += delta
	if m.selectCursor < 0 {
		m.selectCursor = 0
	}
	if m.selectCursor >= len(indexes) {
		m.selectCursor = len(indexes) - 1
	}
	m.refreshViewportContent()
	return m
}

func (m Model) copySelection() (tea.Model, tea.Cmd) {
	indexes := m.selectableMessageIndexes()
	if len(indexes) == 0 || m.selectCursor < 0 || m.selectCursor >= len(indexes) {
		return m.showToast("暂无可复制的选择内容")
	}
	start, end := m.selectAnchor, m.selectCursor
	if start < 0 {
		start = end
	}
	if start > end {
		start, end = end, start
	}
	var content strings.Builder
	for i := start; i <= end; i++ {
		msg := m.messages[indexes[i]]
		prefix := "你"
		if msg.role == "assistant" {
			prefix = "Agent"
		}
		fmt.Fprintf(&content, "%s: %s\n\n", prefix, strings.TrimSpace(msg.content))
	}
	text := strings.TrimSpace(content.String())
	if err := clipboard.Write(text); err != nil {
		return m.showCopyFailureToast(err)
	}
	m = m.exitSelectMode()
	return m.showToast("已复制选择的消息")
}

type slashCommand struct {
	value       string
	description string
}

var slashCommands = []slashCommand{
	{value: "/clear", description: "清空当前会话"},
	{value: "/copy", description: "复制最近回答"},
	{value: "/copy all", description: "复制全部对话"},
	{value: "/copy user", description: "复制最近输入"},
	{value: "/execute", description: "批准实施方案并退出规划模式"},
	{value: "/exit", description: "退出 TUI"},
	{value: "/help", description: "打开帮助"},
	{value: "/plan", description: "进入规划模式"},
	{value: "/plan cancel", description: "放弃规划，回到执行中（无交接）"},
	{value: "/quit", description: "退出 TUI"},
	{value: "/remember", description: "保存长期记忆"},
	{value: "/resume", description: "恢复指定会话"},
}

func (m Model) completeSlashCommand() Model {
	input := strings.TrimSpace(m.textarea.Value())
	if !strings.HasPrefix(input, "/") {
		m.commandSuggestionIndex = -1
		m.commandSuggestionPrefix = ""
		return m
	}
	prefix := input
	if m.commandSuggestionIndex >= 0 && m.commandSuggestionPrefix != "" {
		prefix = m.commandSuggestionPrefix
	} else {
		m.commandSuggestionPrefix = input
	}
	candidates := matchingSlashCommands(prefix)
	if len(candidates) == 0 {
		m.commandSuggestionIndex = -1
		m.commandSuggestionPrefix = ""
		return m
	}
	m.commandSuggestionIndex = (m.commandSuggestionIndex + 1) % len(candidates)
	completion := candidates[m.commandSuggestionIndex].value
	if completion == "/copy" || completion == "/remember" || completion == "/resume" || completion == "/plan" {
		completion += " "
	}
	m.textarea.SetValue(completion)
	m.textarea.CursorEnd()
	return m
}

func matchingSlashCommands(query string) []slashCommand {
	query = strings.ToLower(strings.TrimSpace(query))
	if !strings.HasPrefix(query, "/") {
		return nil
	}
	commands := make([]slashCommand, 0, len(slashCommands))
	for _, command := range slashCommands {
		if strings.HasPrefix(command.value, query) {
			commands = append(commands, command)
		}
	}
	return commands
}

func (m *Model) syncCommandMenu() {
	query := strings.TrimSpace(m.textarea.Value())
	commands := matchingSlashCommands(query)
	if len(commands) == 0 || m.loading || m.helpOverlay || m.selectMode {
		m.commandMenuOpen = false
		m.commandMenuIndex = 0
		m.commandMenuQuery = ""
		m.viewport.Height = m.viewportHeight()
		return
	}
	if !m.commandMenuOpen || m.commandMenuQuery != query {
		m.commandMenuIndex = 0
	}
	m.commandMenuOpen = true
	m.commandMenuQuery = query
	if m.commandMenuIndex >= len(commands) {
		m.commandMenuIndex = len(commands) - 1
	}
	m.viewport.Height = m.viewportHeight()
}

func (m Model) commandMenuCommands() []slashCommand {
	if !m.commandMenuOpen {
		return nil
	}
	return matchingSlashCommands(m.commandMenuQuery)
}

func (m Model) commandMenuHeight() int {
	commands := m.commandMenuCommands()
	if len(commands) == 0 {
		return 0
	}
	visible := len(commands)
	if visible > 5 {
		visible = 5
	}
	return visible + 1
}

func (m Model) moveCommandMenu(delta int) Model {
	commands := m.commandMenuCommands()
	if len(commands) == 0 {
		return m
	}
	m.commandMenuIndex = (m.commandMenuIndex + delta + len(commands)) % len(commands)
	return m
}

func (m Model) applyCommandMenuSelection() Model {
	commands := m.commandMenuCommands()
	if len(commands) == 0 {
		return m
	}
	selection := commands[m.commandMenuIndex].value
	if selection == "/copy" || selection == "/remember" || selection == "/resume" {
		selection += " "
	}
	m.textarea.SetValue(selection)
	m.textarea.CursorEnd()
	m.commandMenuOpen = false
	m.commandMenuQuery = ""
	m.viewport.Height = m.viewportHeight()
	return m
}

func (m Model) sandboxLabel() string {
	if m.svc == nil || m.svc.Config() == nil {
		return "sandbox:unknown"
	}
	cfg := m.svc.Config().Sandbox
	if !cfg.Enabled || cfg.DefaultExecution == "disabled" {
		return "sandbox:off"
	}
	if cfg.Mode != "" {
		return "sandbox:" + cfg.Mode
	}
	return "sandbox:on"
}

// contextUsageLabel 根据当前可见 UI 气泡估算占用（非真实 LLM 装配用量）。
// 真实截断/压缩以 Runtime TokenEstimator + ContextBudgetPlanner 为准；此处仅作交互提示。
func (m Model) contextUsageLabel() string {
	if m.svc == nil || m.svc.Config() == nil {
		return ""
	}
	resolved, err := m.svc.Config().LLM.ResolveRoute("chat")
	if err != nil || resolved.ContextWindow <= 0 {
		return ""
	}
	chars := 0
	for _, message := range m.messages {
		chars += len([]rune(message.content))
	}
	// 对中英文混合文本采用保守近似：每 2 个字符约为 1 token。
	estimatedTokens := (chars + 1) / 2
	pct := estimatedTokens * 100 / resolved.ContextWindow
	if pct > 100 {
		pct = 100
	}
	// ui-ctx：强调这是可见 transcript 近似，避免与模型真实 context 混淆。
	return fmt.Sprintf("ui-ctx~%d%%", pct)
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

func flushProgressAfter(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return flushProgressMsg{}
	})
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
			// 必须可靠投递：丢弃 PhaseStart 会导致 final_answer 无法分段，对话区出现叠加/重复。
			select {
			case progressCh <- progressMsg{event: event}:
			case <-m.ctx.Done():
			}
		}
		result, err := m.svc.RunOnce(m.ctx, app.RunRequest{
			SessionID:  m.session.ID,
			AppID:      m.session.AppID,
			TenantID:   m.session.TenantID,
			UserID:     m.session.UserID,
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
	if m.svc == nil {
		return "unknown"
	}
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
	if m.session == nil {
		return "unknown"
	}
	if len(m.session.ID) > 22 {
		return m.session.ID[:22] + "…"
	}
	return m.session.ID
}

// helpText /help 命令显示的帮助内容
const helpText = `可用命令（以 / 开头）:
  /remember <内容>  让智能体主动记住个人偏好或代码知识
  /clear            清空当前会话历史，开始新对话
  /copy             复制最近一次 Agent 的最终回答到剪贴板
  /copy user        复制最近一次用户消息到剪贴板
  /copy all         复制整段可见对话（纯文本格式）到剪贴板
  /resume [ID]      恢复指定会话；省略 ID 时恢复最近会话
  /sessions         列出最近会话
  /help             显示此帮助
  /quit             退出程序

直接输入以下前缀也可让智能体记住内容：
  #记住 <内容>      示例: #记住 数据库连接首选 PostgreSQL
  #remember <内容>

快捷键:
  Enter    发送消息
  Esc      推理中取消本轮 / 空闲时清空输入框
  Ctrl+C   推理中取消本轮 / 空闲时按两次退出
  Ctrl+D   退出程序
  Ctrl+Y   复制最近一次 Agent 回答到系统剪贴板
  /        自动显示命令菜单；↑/↓ 选择，Enter 使用
  Tab      补全当前斜杠命令
  v        输入为空时进入消息选择模式
  j / k    选择模式中移动消息
  y        选择模式中复制消息
  p        展开/折叠计划卡片（输入区域为空时生效）
  ↑ / ↓    输入为空时滚动消息历史（一行）
  PgUp     向上翻页
  PgDn     向下翻页
  鼠标滚轮 滚动消息历史
  Ctrl+P/N 上一条/下一条输入历史
  Shift+拖选 终端原生选取文本（启用鼠标捕获后需按住 Shift）`

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
