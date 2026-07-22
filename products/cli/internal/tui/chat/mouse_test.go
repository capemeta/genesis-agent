package chat

import (
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
)

func TestByteOffsetAtCol(t *testing.T) {
	cases := []struct {
		name string
		line string
		col  int
		want int
	}{
		{"ascii-start", "Hello", 0, 0},
		{"ascii-mid", "Hello", 2, 2},
		{"ascii-beyond", "Hello", 10, 5},
		{"cjk-start", "世界", 0, 0},
		{"cjk-after-first", "世界", 2, 3},   // 全角宽 2，col=2 落在第二个字前
		{"cjk-mid-rounds-up", "世界", 1, 3}, // col 落在首字中间，按 rune 边界向后取整
		{"cjk-end", "世界", 4, 6},           // 两字共 6 字节
		{"mixed", "a世b", 3, 4},            // a(1) 世(2) → col3 落在 b 前，字节 4
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := byteOffsetAtCol(c.line, c.col); got != c.want {
				t.Fatalf("byteOffsetAtCol(%q,%d)=%d, want %d", c.line, c.col, got, c.want)
			}
		})
	}
}

func TestSelectedTextSingleLine(t *testing.T) {
	m := Model{
		renderedPlainLines: []string{"  你好", "  Hello 世界 foobar   "},
		mouseSelActive:     true,
		selStartLine:       1, selStartCol: 2, // "Hello..." 起点（跳过前导缩进）
		selEndLine: 1, selEndCol: 7, // 到 "Hello" 之后
	}
	got := m.selectedText()
	if got != "Hello" {
		t.Fatalf("selectedText single line = %q, want %q", got, "Hello")
	}
}

func TestSelectedTextMultiLineTrimsTrailingPadding(t *testing.T) {
	// 起点在首行跳过前导缩进；续行按屏幕原样（保留前导缩进）；尾随填充空格去除。
	m := Model{
		renderedPlainLines: []string{
			"  你                    ",
			"  第一行内容",
			"  第二行内容更长一些",
		},
		mouseSelActive: true,
		selStartLine:   0, selStartCol: 2,
		selEndLine: 2, selEndCol: 100, // 越过行尾，取到末尾
	}
	got := m.selectedText()
	want := "你\n  第一行内容\n  第二行内容更长一些"
	if got != want {
		t.Fatalf("selectedText multi line = %q, want %q", got, want)
	}
}

func TestSelectedTextNormalizesReverseSelection(t *testing.T) {
	// 终点在起点之前（从下往上拖），应规整为正序输出。
	m := Model{
		renderedPlainLines: []string{"  abc", "  def"},
		mouseSelActive:     true,
		selStartLine:       1, selStartCol: 5, // def 结束处
		selEndLine: 0, selEndCol: 2, // abc 起点（跳过缩进）
	}
	got := m.selectedText()
	want := "abc\n  def"
	if got != want {
		t.Fatalf("selectedText reverse = %q, want %q", got, want)
	}
}

func TestScreenToContentMapsViewportOffset(t *testing.T) {
	m := Model{
		renderedPlainLines: []string{"l0", "l1", "l2", "l3", "l4", "l5"},
	}
	m.viewport = viewport.New(20, 3)
	m.viewport.SetContent("l0\nl1\nl2\nl3\nl4\nl5")
	m.viewport.SetYOffset(2) // 顶部显示 l2

	// 屏幕 Y = headerLines + 1 → 第二可见行 → 内容行 = YOffset(2)+1 = 3
	line, col, ok := m.screenToContent(4, headerLines+1)
	if !ok {
		t.Fatalf("screenToContent ok=false, want true")
	}
	if line != 3 {
		t.Fatalf("line=%d, want 3", line)
	}
	if col != 4 {
		t.Fatalf("col=%d, want 4", col)
	}

	// Y 在消息区之上（标题栏区域）应越界。
	if _, _, ok := m.screenToContent(4, headerLines-1); ok {
		t.Fatalf("screenToContent above viewport should be out of range")
	}
	// Y 超出 viewport 高度应越界。
	if _, _, ok := m.screenToContent(4, headerLines+3); ok {
		t.Fatalf("screenToContent below viewport should be out of range")
	}
}
