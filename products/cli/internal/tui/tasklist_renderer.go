// Package tui 提供 CLI 交互界面的终端渲染组件
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"genesis-agent/internal/capabilities/tasklist/model"
	"genesis-agent/products/cli/internal/tui/styles"
)

var (
	styleCompleted = lipgloss.NewStyle().Foreground(styles.ColorGreen)
	styleProgress  = lipgloss.NewStyle().Foreground(styles.ColorAccent).Bold(true)
	stylePending   = lipgloss.NewStyle().Foreground(styles.ColorGray)
	styleBlocked   = lipgloss.NewStyle().Foreground(styles.ColorYellow).Bold(true)
	styleNotes     = lipgloss.NewStyle().Foreground(styles.ColorGray).Italic(true)
	styleHeader    = lipgloss.NewStyle().Foreground(styles.ColorWhite).Bold(true)
)

// RenderPlan 渲染任务清单为格式化的 TUI 字符串
func RenderPlan(plan *model.TaskList) string {
	if plan == nil || len(plan.Steps) == 0 {
		return stylePending.Render(" 📭 [无任务清单]")
	}

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(styleHeader.Render(fmt.Sprintf(" 📋 任务清单 (版本 %d):", plan.Version)))
	if plan.LatestExplanation != "" {
		sb.WriteString(fmt.Sprintf("\n   %s", styleNotes.Render("💡 说明: "+plan.LatestExplanation)))
	}
	sb.WriteString("\n")

	// 构建树状关系
	roots := make([]model.Step, 0)
	childrenMap := make(map[string][]model.Step)

	for _, step := range plan.Steps {
		if step.ParentID == "" {
			roots = append(roots, step)
		} else {
			childrenMap[step.ParentID] = append(childrenMap[step.ParentID], step)
		}
	}

	// 如果全部节点都有 parent_id 但找不到根（极端异常），就直接当做平铺平铺处理
	if len(roots) == 0 && len(plan.Steps) > 0 {
		roots = plan.Steps
	}

	// 递归渲染每一棵子树
	for i, root := range roots {
		isLastRoot := i == len(roots)-1
		renderTreeStep(&sb, root, "", isLastRoot, childrenMap)
	}

	return sb.String()
}

func renderTreeStep(sb *strings.Builder, step model.Step, prefix string, isLast bool, childrenMap map[string][]model.Step) {
	// 选择当前分支连接符
	connector := " ├── "
	if isLast {
		connector = " └── "
	}

	// 根据状态匹配符号与样式
	var symbol string
	var textStyle lipgloss.Style

	switch step.Status {
	case model.StepStatusCompleted:
		symbol = "✔"
		textStyle = styleCompleted
	case model.StepStatusInProgress:
		symbol = "⏵"
		textStyle = styleProgress
	case model.StepStatusBlockedByApproval:
		symbol = "⚠"
		textStyle = styleBlocked
	default:
		symbol = "□"
		textStyle = stylePending
	}

	// 渲染当前节点
	sb.WriteString(prefix)
	sb.WriteString(connector)
	sb.WriteString(textStyle.Render(fmt.Sprintf("[%s] %s", symbol, step.Title)))
	if step.Notes != "" {
		sb.WriteString(" ")
		sb.WriteString(styleNotes.Render("(" + step.Notes + ")"))
	}
	sb.WriteString("\n")

	// 计算给子节点的 prefix 缩进
	childPrefix := prefix + " │   "
	if isLast {
		childPrefix = prefix + "     "
	}

	// 渲染子节点
	children := childrenMap[step.ID]
	for idx, child := range children {
		isLastChild := idx == len(children)-1
		renderTreeStep(sb, child, childPrefix, isLastChild, childrenMap)
	}
}
