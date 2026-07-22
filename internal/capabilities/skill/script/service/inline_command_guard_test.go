package service

import "testing"

func TestDetectRiskyInlineCommandMultiline(t *testing.T) {
	cmd := "python -c \"\nfrom openpyxl import load_workbook\nwb = load_workbook('sales.xlsx')\n\""
	risk, ok := detectRiskyInlineCommand(cmd)
	if !ok || risk.Reason != "multiline" || risk.Kind != "python_c" {
		t.Fatalf("got ok=%v risk=%+v", ok, risk)
	}
}

func TestDetectRiskyInlineCommandTooLong(t *testing.T) {
	payload := stringsRepeat("x", maxSafeInlinePayloadRunes+1)
	cmd := `python -c "` + payload + `"`
	risk, ok := detectRiskyInlineCommand(cmd)
	if !ok || risk.Reason != "too_long" {
		t.Fatalf("got ok=%v risk=%+v", ok, risk)
	}
}

func TestDetectRiskyInlineCommandEscapedQuotes(t *testing.T) {
	cmd := "node -e \"console.log(\\\"ok\\\")\""
	risk, ok := detectRiskyInlineCommand(cmd)
	if !ok || risk.Reason != "escaped_quotes" {
		t.Fatalf("got ok=%v risk=%+v", ok, risk)
	}
}

func TestDetectRiskyInlineCommandAllowsShortProbe(t *testing.T) {
	for _, cmd := range []string{
		`python -c "import openpyxl; print('ok')"`,
		`python3 -c "print(1)"`,
		`node -e "require('docx')"`,
	} {
		if risk, ok := detectRiskyInlineCommand(cmd); ok {
			t.Fatalf("should allow short probe %q got %+v", cmd, risk)
		}
	}
}

func TestDetectRiskyInlineCommandIgnoresNormalScripts(t *testing.T) {
	if risk, ok := detectRiskyInlineCommand("python check_sales.py"); ok {
		t.Fatalf("unexpected %+v", risk)
	}
	if risk, ok := detectRiskyInlineCommand("node create_doc.js"); ok {
		t.Fatalf("unexpected %+v", risk)
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func TestAutoRewriteRiskyInlineCommand(t *testing.T) {
	// 1. 常规单行探测 (<= 1024 字符，无换行/转义)：不触发重写，直接原样内联执行
	shortCmd := `python -c "import openpyxl; print('ok')"`
	if _, _, _, ok := autoRewriteRiskyInlineCommand(shortCmd); ok {
		t.Fatalf("short command %q should NOT be auto-rewritten", shortCmd)
	}

	// 2. 长内联指令 (> 1024 字符)：自动重写落盘为脚本文件
	longPayload := stringsRepeat("x", 1100)
	cmd := `python -m markitdown "test.pptx" | python -c "` + longPayload + `"`
	rewritten, scriptName, payload, ok := autoRewriteRiskyInlineCommand(cmd)
	if !ok {
		t.Fatalf("expected autoRewriteRiskyInlineCommand ok=true for long command")
	}
	if scriptName != "_auto_inline_check.py" {
		t.Errorf("unexpected scriptName: %s", scriptName)
	}
	if payload != longPayload {
		t.Errorf("unexpected payload: %s", payload)
	}
	wantCmd := `python -m markitdown "test.pptx" | python _auto_inline_check.py`
	if rewritten != wantCmd {
		t.Errorf("expected rewritten cmd %q, got %q", wantCmd, rewritten)
	}
}

