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
	Background(ColorPrimary).
	Foreground(ColorWhite).
	Bold(true).
	Padding(0, 2)

// HeaderBarSub 标题栏次要信息区（模型名、策略名等）
var HeaderBarSub = lipgloss.NewStyle().
	Background(ColorSecondary).
	Foreground(lipgloss.Color("#C4B5FD")).
	Padding(0, 2)

// HeaderBarFill 标题栏右侧填充（保持满宽背景色）
var HeaderBarFill = lipgloss.NewStyle().
	Background(ColorPrimary)

// ── 消息气泡 ─────────────────────────────────────────────────

// UserLabel 用户消息发送者标签（"你"）
var UserLabel = lipgloss.NewStyle().
	Foreground(ColorBlue).
	Bold(true)

// UserBubble 用户消息内容区
var UserBubble = lipgloss.NewStyle().
	Foreground(ColorWhite).
	Background(ColorBlue).
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

// HelpBar 帮助栏普通文字
var HelpBar = lipgloss.NewStyle().
	Foreground(ColorGray)

// HelpKey 帮助栏快捷键高亮
var HelpKey = lipgloss.NewStyle().
	Foreground(ColorAccent).
	Bold(true)

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
