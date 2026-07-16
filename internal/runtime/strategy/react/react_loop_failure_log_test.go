package react

import (
	"errors"
	"strings"
	"testing"
)

func TestExtractToolFailureLogFieldsFromJSON(t *testing.T) {
	result := `{"ok":false,"failure_kind":"dependency_missing","stdout":"","stderr":"Cannot find module 'pptxgenjs'","error":"script exit_code=1"}`
	kind, stdout, stderr := extractToolFailureLogFields(result, errors.New("exit status 1"))
	if kind != "dependency_missing" {
		t.Fatalf("kind=%q", kind)
	}
	if !strings.Contains(stderr, "pptxgenjs") {
		t.Fatalf("stderr=%q", stderr)
	}
	_ = stdout
}

func TestExtractToolFailureLogFieldsInfersPathContract(t *testing.T) {
	kind, _, _ := extractToolFailureLogFields("", errors.New("invalid_input: EXECUTION_PATH_CONTRACT_VIOLATION: /"))
	if kind != "path_contract_violation" {
		t.Fatalf("kind=%q", kind)
	}
}

func TestSummarizeToolResultForLogOmitsFullStdoutAndStderr(t *testing.T) {
	result := `{"ok":true,"stdout":"高度敏感的PDF正文","stderr":"token=super-secret","nested":{"stdout":"nested text"}}`
	got := summarizeToolResultForLog(result)
	for _, secret := range []string{"高度敏感的PDF正文", "super-secret", "nested text"} {
		if strings.Contains(got, secret) {
			t.Fatalf("日志不应包含原始输出 %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "sha256=") || !strings.Contains(got, "bytes=") {
		t.Fatalf("日志摘要应保留长度与哈希: %s", got)
	}
}

func TestSummarizeToolResultForLogOmitsContent(t *testing.T) {
	got := summarizeToolResultForLog(`{"resource":"demo/guide.md","content":"敏感文档正文"}`)
	if strings.Contains(got, "敏感文档正文") || !strings.Contains(got, "sha256=") {
		t.Fatalf("content should be summarized: %s", got)
	}
}

func TestSummarizeToolArgumentsForLogOmitsWritePayload(t *testing.T) {
	got := summarizeToolArgumentsForLog(`{"path":"$WORK_DIR/a.js","content":"敏感脚本正文"}`)
	if strings.Contains(got, "敏感脚本正文") || !strings.Contains(got, `$WORK_DIR/a.js`) || !strings.Contains(got, "sha256=") {
		t.Fatalf("arguments should preserve metadata only: %s", got)
	}
}

func TestSummarizeToolArgumentsForLogOmitsInvalidJSON(t *testing.T) {
	got := summarizeToolArgumentsForLog(`{"path":"a.js","content":"敏感且截断`)
	if strings.Contains(got, "敏感且截断") || !strings.Contains(got, "sha256=") {
		t.Fatalf("invalid arguments should be fully summarized: %s", got)
	}
}

func TestSummarizeToolOutputForLogRedactsAndTruncatesFailurePreview(t *testing.T) {
	got := summarizeToolOutputForLog("password=super-secret\nCannot find module pdfplumber", 500)
	if strings.Contains(got, "super-secret") || !strings.Contains(got, "password=[redacted]") {
		t.Fatalf("输出未脱敏: %s", got)
	}
	if !strings.Contains(got, "pdfplumber") || !strings.Contains(got, "sha256=") {
		t.Fatalf("失败诊断或哈希缺失: %s", got)
	}
}
