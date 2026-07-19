package attach

import "testing"

func TestParseAtMentions(t *testing.T) {
	t.Parallel()
	clean, paths := ParseAtMentions(`分析 @slide.png 和 @docs/a.docx 谢谢`)
	if clean != "分析 和 谢谢" {
		t.Fatalf("clean=%q", clean)
	}
	if len(paths) != 2 || paths[0] != "slide.png" || paths[1] != "docs/a.docx" {
		t.Fatalf("paths=%v", paths)
	}
}
