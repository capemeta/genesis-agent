// Package styles 定义 CLI TUI 的 Lip Gloss 视觉样式
// 统一管理颜色、排版、边框等视觉元素，遵循 Genesis Agent 紫色主题
package styles

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── 调色板（Genesis Agent 紫色主题）───────────────────────────

var (
	// ColorPrimary 主色：紫罗兰（标题、标签、聚焦边框）
	ColorPrimary = lipgloss.Color("#7C3AED")
	// ColorSecondary 次色：深紫（标题栏次要区域）
	ColorSecondary = lipgloss.Color("#6D28D9")
	// ColorAccent 强调色：浅紫（快捷键高亮、辅助元素）
	ColorAccent = lipgloss.Color("#8B5CF6")
	// ColorGray 辅助灰（次要文字、帮助栏）
	ColorGray = lipgloss.Color("#9CA3AF")
	// ColorDarkGray 深灰（边框、分隔线）
	ColorDarkGray = lipgloss.Color("#4B5563")
	// ColorGreen 成功绿（确认、成功提示）
	ColorGreen = lipgloss.Color("#10B981")
	// ColorRed 错误红（错误提示）
	ColorRed = lipgloss.Color("#EF4444")
	// ColorYellow 警告黄（加载动画、警告）
	ColorYellow = lipgloss.Color("#F59E0B")
	// ColorBlue 用户消息蓝
	ColorBlue = lipgloss.Color("#3B82F6")
	// ColorWhite 主文本白
	ColorWhite = lipgloss.Color("#F9FAFB")
)

// ── 标题栏 ──────────────────────────────────────────────────

// HeaderBar 标题栏主区域（程序名）
var HeaderBar = lipgloss.NewStyle().
	Foreground(ColorPrimary).
	Bold(true).
	Padding(0, 1)

// HeaderBarSub 标题栏次要信息区（模型名、策略名等）
var HeaderBarSub = lipgloss.NewStyle().
	Foreground(ColorGray).
	Padding(0, 1)

// HeaderChip 顶栏紧凑状态标签。
var HeaderChip = lipgloss.NewStyle().
	Foreground(ColorAccent).
	Background(lipgloss.Color("#1F2937")).
	Padding(0, 1)

// ── 消息气泡 ─────────────────────────────────────────────────

// UserLabel 用户消息发送者标签（"你"）
var UserLabel = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true)

// UserBubble 用户消息内容区（无背景气泡，扁平化）
var UserBubble = lipgloss.NewStyle().
	Foreground(ColorWhite).
	Padding(0, 1).
	MarginBottom(1)

// AgentLabel Agent 回复发送者标签（"Agent"）
var AgentLabel = lipgloss.NewStyle().
	Foreground(ColorPrimary).
	Bold(true)

// AgentBubble Agent 回复内容区
var AgentBubble = lipgloss.NewStyle().
	Foreground(ColorWhite).
	Padding(0, 1).
	MarginBottom(1)

// SystemMsg 系统消息（/help 响应、错误信息等）
var SystemMsg = lipgloss.NewStyle().
	Foreground(ColorGray).
	Italic(true).
	Padding(0, 1)

// HelpOverlay 让帮助在固定 transcript 区内展示，不改变整体布局高度。
var HelpOverlay = lipgloss.NewStyle().
	Foreground(ColorGray).
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorDarkGray).
	Padding(1, 2)

// CommandMenu 样式用于 Composer 上方的斜杠命令选择菜单。
var CommandMenuCommand = lipgloss.NewStyle().
	Foreground(ColorWhite).
	Bold(true)

var CommandMenuDescription = lipgloss.NewStyle().
	Foreground(ColorGray)

var CommandMenuSelected = lipgloss.NewStyle().
	Foreground(ColorAccent).
	Bold(true)

var CommandMenuHint = lipgloss.NewStyle().
	Foreground(ColorDarkGray)

// MetaInfo 消息元信息（步骤数、token 消耗、耗时）
var MetaInfo = lipgloss.NewStyle().
	Foreground(ColorDarkGray).
	Italic(true)

// ── 状态 / 帮助栏 ───────────────────────────────────────────

// StatusLoading 加载状态文字（配合 Spinner 使用）
var StatusLoading = lipgloss.NewStyle().
	Foreground(ColorYellow)

// StatusError 错误状态文字
var StatusError = lipgloss.NewStyle().
	Foreground(ColorRed).
	Bold(true)

// StatusToast 提示状态文字（Toast，如复制成功）
var StatusToast = lipgloss.NewStyle().
	Foreground(ColorGreen).
	Bold(true)

// HelpBar 帮助栏普通文字
var HelpBar = lipgloss.NewStyle().
	Foreground(ColorGray)

// HelpKey 帮助栏快捷键高亮
var HelpKey = lipgloss.NewStyle().
	Foreground(ColorAccent).
	Bold(true)

// SelectionCursor 和 SelectionRange 区分选择模式的当前光标与已选范围。
var SelectionCursor = lipgloss.NewStyle().
	Foreground(ColorYellow).
	Bold(true)

var SelectionRange = lipgloss.NewStyle().
	Foreground(ColorAccent)

// ── 输入框 ───────────────────────────────────────────────────

// InputBorder 输入框圆角边框（未聚焦，灰色）
var InputBorder = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(ColorDarkGray).
	Padding(0, 1)

// InputBorderFocused 输入框圆角边框（聚焦，主色紫）
var InputBorderFocused = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(ColorPrimary).
	Padding(0, 1)

// ── 工具函数 ─────────────────────────────────────────────────

// Divider 创建指定宽度的水平分隔线（深灰色）
func Divider(width int) string {
	if width <= 0 {
		return ""
	}
	line := strings.Repeat("─", width)
	return lipgloss.NewStyle().Foreground(ColorDarkGray).Render(line)
}

// ── 计划卡片 ─────────────────────────────────────────────

// PlanBorder 计划卡片圆角边框（蓝色，与审批卡黄色区分）
var PlanBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorBlue).
	Padding(0, 2)

// PlanTitle 计划标题样式
var PlanTitle = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true)

// PlanProgressBar 进度条文字样式
var PlanProgressBar = lipgloss.NewStyle().
	Foreground(ColorGray)

// PlanItemDone 已完成条目样式（绿色）
var PlanItemDone = lipgloss.NewStyle().
	Foreground(ColorGreen)

// PlanItemDoing 进行中条目样式（黄色加粗）
var PlanItemDoing = lipgloss.NewStyle().
	Foreground(ColorYellow).
	Bold(true)

// PlanItemPending 待开始条目样式（深灰）
var PlanItemPending = lipgloss.NewStyle().
	Foreground(ColorDarkGray)

// PlanItemFailed 失败条目样式（红色）
var PlanItemFailed = lipgloss.NewStyle().
	Foreground(ColorRed)

// PlanHint 计划卡片底部提示文字
var PlanHint = lipgloss.NewStyle().
	Foreground(ColorDarkGray).
	Italic(true)

// ProgressBar 生成文字进度条
// filled: "█"，empty: "░"，width: 进度条字符宽度
func ProgressBar(pct, width int) string {
	if width <= 0 {
		return ""
	}
	filled := width * pct / 100
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	color := ColorGreen
	if pct < 100 {
		color = ColorBlue
	}
	return lipgloss.NewStyle().Foreground(color).Render(bar)
}
