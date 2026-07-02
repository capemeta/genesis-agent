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
	messages []uiMessage // 当前会话所有消息（用于 View 渲染）
	loading  bool        // 是否正在等待 Agent 推理响应
	err      error       // 最近一次错误（nil 表示无错误）

	// ── 布局状态 ──────────────────────────────────────────────
	width  int  // 当前终端宽度（通过 WindowSizeMsg 更新）
	height int  // 当前终端高度（通过 WindowSizeMsg 更新）
	ready  bool // viewport 是否已完成首次初始化
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
	m.viewport.SetContent(renderMessages(m.messages, m.width))
}

// runAgentCmd 构造一个 Bubble Tea Cmd，在后台 goroutine 中执行 Agent 推理
// 推理结果通过 runCompleteMsg 或 runErrorMsg 发回 Update 循环
func (m Model) runAgentCmd(input string) tea.Cmd {
	return func() tea.Msg {
		result, err := m.svc.RunOnce(m.ctx, app.RunRequest{
			SessionID: m.session.ID,
			TenantID:  m.session.TenantID,
			Input:     input,
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
