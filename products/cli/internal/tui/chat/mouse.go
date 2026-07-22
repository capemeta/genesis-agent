package chat

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"genesis-agent/products/cli/internal/tui/clipboard"
	"genesis-agent/products/cli/internal/tui/styles"
)

// handleMouse 处理消息区的鼠标事件。
//
// 背景：TUI 启用了 WithMouseCellMotion 以支持滚轮滚动，代价是终端把所有鼠标事件
// 转发给应用，原生拖选被拦截。因此这里在应用内实现字符级拖选并复制：
//   - 滚轮事件：转交 viewport 滚动（保持原有行为）；
//   - 左键按下：记录选区锚点；
//   - 拖动（motion，仅在按住时上报）：更新选区光标并叠加高亮，越界时自动滚动；
//   - 松开：把选中文本写入系统剪贴板并清除高亮。
//
// 坐标换算：消息区 viewport 位于标题栏（headerLines 行）之下，且 viewport 内容
// 不做软换行（1 内容行 = 1 可见行），故：
//
//	内容行下标 = viewport.YOffset + (屏幕Y - headerLines)
//	可见列     = 屏幕X（viewport 从第 0 列起绘制，无水平偏移）
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// 滚轮：保持原有滚动行为，交给 viewport 处理。
	// 注意：IsWheel 定义在底层 MouseEvent 上，需显式转换。
	if tea.MouseEvent(msg).IsWheel() {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	// 帮助覆盖层 / 审批拦截 / 键盘消息选择模式下不启用鼠标拖选，避免语义冲突。
	if m.helpOverlay || m.approvalFocus || m.selectMode {
		return m, nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return m, nil
		}
		line, col, ok := m.screenToContent(msg.X, msg.Y)
		if !ok {
			// 点击在消息区之外（标题/状态/输入等）：清除可能残留的高亮。
			if m.mouseSelActive {
				m = m.clearMouseSelection()
			}
			return m, nil
		}
		m.mouseSelecting = true
		m.mouseSelActive = false // 仅当产生位移后才视为有效选区，避免单击闪烁。
		m.selStartLine, m.selStartCol = line, col
		m.selEndLine, m.selEndCol = line, col
		return m, nil

	case tea.MouseActionMotion:
		if !m.mouseSelecting {
			return m, nil
		}
		m.autoScrollDuringDrag(msg.Y)
		line, col := m.screenToContentClamped(msg.X, msg.Y)
		m.selEndLine, m.selEndCol = line, col
		if line != m.selStartLine || col != m.selStartCol {
			m.mouseSelActive = true
		}
		m.applyMouseSelectionHighlight()
		return m, nil

	case tea.MouseActionRelease:
		if !m.mouseSelecting {
			return m, nil
		}
		m.mouseSelecting = false
		if !m.mouseSelActive {
			// 单击未拖动：不复制，仅确保无残留高亮。
			m = m.clearMouseSelection()
			return m, nil
		}
		text := m.selectedText()
		m = m.clearMouseSelection()
		if strings.TrimSpace(text) == "" {
			return m, nil
		}
		if err := clipboard.Write(text); err != nil {
			return m.showCopyFailureToast(err)
		}
		return m.showToast("✓ 已复制选中文本")
	}

	return m, nil
}

// screenToContent 将屏幕坐标映射到消息区内容坐标；越界返回 ok=false。
func (m Model) screenToContent(x, y int) (line, col int, ok bool) {
	top := headerLines
	if y < top || y >= top+m.viewport.Height {
		return 0, 0, false
	}
	line = m.viewport.YOffset + (y - top)
	if line < 0 || line >= len(m.renderedPlainLines) {
		return 0, 0, false
	}
	if x < 0 {
		x = 0
	}
	return line, x, true
}

// screenToContentClamped 与 screenToContent 类似，但把坐标钳制到有效范围，
// 用于拖动时即使指针移出消息区也能得到边界上的合理选区端点。
func (m Model) screenToContentClamped(x, y int) (line, col int) {
	top := headerLines
	rel := y - top
	if rel < 0 {
		rel = 0
	}
	if rel >= m.viewport.Height {
		rel = m.viewport.Height - 1
	}
	line = m.viewport.YOffset + rel
	if line < 0 {
		line = 0
	}
	if maxLine := len(m.renderedPlainLines) - 1; maxLine >= 0 && line > maxLine {
		line = maxLine
	}
	if x < 0 {
		x = 0
	}
	return line, x
}

// autoScrollDuringDrag 拖动越过消息区上/下边界时自动滚动一行，方便跨屏选取。
func (m *Model) autoScrollDuringDrag(y int) {
	top := headerLines
	switch {
	case y < top:
		m.viewport.LineUp(1)
	case y >= top+m.viewport.Height:
		m.viewport.LineDown(1)
	}
}

// normalizedSelection 返回按阅读顺序排列的选区端点（起点 <= 终点）。
func (m Model) normalizedSelection() (startLine, startCol, endLine, endCol int) {
	if m.selStartLine < m.selEndLine ||
		(m.selStartLine == m.selEndLine && m.selStartCol <= m.selEndCol) {
		return m.selStartLine, m.selStartCol, m.selEndLine, m.selEndCol
	}
	return m.selEndLine, m.selEndCol, m.selStartLine, m.selStartCol
}

// applyMouseSelectionHighlight 基于去 ANSI 的可见行重建消息区内容并叠加反显高亮。
// 拖选期间以纯文本 + 反显呈现，使选区清晰可见；松开或清除后由 refreshViewportContent
// 恢复完整着色内容。
func (m *Model) applyMouseSelectionHighlight() {
	if !m.mouseSelActive || len(m.renderedPlainLines) == 0 {
		return
	}
	startLine, startCol, endLine, endCol := m.normalizedSelection()

	var b strings.Builder
	last := len(m.renderedPlainLines) - 1
	for i, line := range m.renderedPlainLines {
		switch {
		case i < startLine || i > endLine:
			b.WriteString(line)
		default:
			s := 0
			e := lineContentWidth(line)
			if i == startLine {
				s = startCol
			}
			if i == endLine {
				e = endCol
			}
			b.WriteString(highlightRange(line, s, e))
		}
		if i < last {
			b.WriteByte('\n')
		}
	}
	m.viewport.SetContent(b.String())
}

// selectedText 提取当前选区覆盖的纯文本（按行拼接，去除每行尾随空白）。
func (m Model) selectedText() string {
	if !m.mouseSelActive || len(m.renderedPlainLines) == 0 {
		return ""
	}
	startLine, startCol, endLine, endCol := m.normalizedSelection()
	if startLine >= len(m.renderedPlainLines) {
		return ""
	}
	if endLine > len(m.renderedPlainLines)-1 {
		endLine = len(m.renderedPlainLines) - 1
	}

	lines := make([]string, 0, endLine-startLine+1)
	for i := startLine; i <= endLine; i++ {
		line := m.renderedPlainLines[i]
		s := 0
		e := len(line)
		if i == startLine {
			s = byteOffsetAtCol(line, startCol)
		}
		if i == endLine {
			e = byteOffsetAtCol(line, endCol)
		}
		if s > e {
			s = e
		}
		lines = append(lines, strings.TrimRight(line[s:e], " "))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n ")
}

// clearMouseSelection 清除选区状态并恢复正常着色内容。
func (m Model) clearMouseSelection() Model {
	m.mouseSelecting = false
	m.mouseSelActive = false
	m.selStartLine, m.selStartCol = 0, 0
	m.selEndLine, m.selEndCol = 0, 0
	m.refreshViewportContent()
	return m
}

// lineContentWidth 返回去掉尾随空格后的可见宽度，避免高亮延伸到 lipgloss 填充区。
func lineContentWidth(line string) int {
	return ansi.StringWidth(strings.TrimRight(line, " "))
}

// highlightRange 对 line 的 [startCol,endCol) 可见列区间叠加反显样式，其余保持原样。
// line 为去 ANSI 的纯文本，列以显示宽度计（兼容 CJK 全角字符）。
func highlightRange(line string, startCol, endCol int) string {
	if endCol <= startCol {
		return line
	}
	s := byteOffsetAtCol(line, startCol)
	e := byteOffsetAtCol(line, endCol)
	if s >= e {
		return line
	}
	return line[:s] + styles.MouseSelection.Render(line[s:e]) + line[e:]
}

// byteOffsetAtCol 返回可见列 col 对应的字节偏移：即第一个起始显示列 >= col 的 rune 边界。
// col 落在某个宽字符中间时按 rune 边界向后取整。
func byteOffsetAtCol(line string, col int) int {
	if col <= 0 {
		return 0
	}
	width := 0
	for idx, r := range line {
		if width >= col {
			return idx
		}
		width += ansi.StringWidth(string(r))
	}
	return len(line)
}
