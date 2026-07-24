package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRotatingWriter_DayRoll(t *testing.T) {
	dir := t.TempDir()
	var now time.Time
	now = time.Date(2026, 7, 8, 10, 0, 0, 0, time.Local)
	w, err := NewRotatingWriter(dir, "agent", RotateOptions{
		Daily: true, MaxSizeMB: 100, RetainDays: 14,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	if _, err := w.Write([]byte("day1\n")); err != nil {
		t.Fatalf("write day1: %v", err)
	}

	now = time.Date(2026, 7, 9, 10, 0, 0, 0, time.Local)
	if _, err := w.Write([]byte("day2\n")); err != nil {
		t.Fatalf("write day2: %v", err)
	}
	_ = w.Close()

	archived, err := listNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(archived, "agent.2026-07-08.log") {
		t.Fatalf("archived = %v, want agent.2026-07-08.log", archived)
	}
	active := filepath.Join(dir, "agent.log")
	data, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "day2\n" {
		t.Fatalf("active content = %q", data)
	}
}

func TestRotatingWriter_SizeRoll(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.Local)
	w, err := NewRotatingWriter(dir, "agent", RotateOptions{
		Daily: true, MaxSizeMB: 1, RetainDays: 14,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}

	payload := strings.Repeat("x", 1024*1024) // 1MB
	if _, err := w.Write([]byte(payload)); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if _, err := w.Write([]byte("more\n")); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	_ = w.Close()

	archived, err := listNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	foundSizeRoll := false
	for _, name := range archived {
		if name == "agent.2026-07-09.1.log" {
			foundSizeRoll = true
			break
		}
	}
	if !foundSizeRoll {
		t.Fatalf("archived = %v, want agent.2026-07-09.1.log for size roll", archived)
	}
	activeData, err := os.ReadFile(filepath.Join(dir, "agent.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(activeData), "more") {
		t.Fatalf("active should contain post-roll write, got %q", truncate(string(activeData), 32))
	}
}

func TestRotatingWriter_Retain(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "agent.2026-01-01.log")
	if err := os.WriteFile(old, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "agent.2026-07-08.log")
	if err := os.WriteFile(keep, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 9, 0, 0, 0, 0, time.Local)
	w, err := NewRotatingWriter(dir, "agent", RotateOptions{
		Daily: true, MaxSizeMB: 100, RetainDays: 14,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	if _, err := w.Write([]byte("now\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = w.Close()

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old archive should be deleted, err=%v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("recent archive should remain: %v", err)
	}
}

func TestRotatingWriter_RotateOnStartAndLazyOpen(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.Local)
	activePath := filepath.Join(dir, "llm.log")

	// 模拟上一次 Session 产生的 llm.log 内容
	if err := os.WriteFile(activePath, []byte("prior session log\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. 初始化 Writer，开启 RotateOnStart 与 LazyOpen
	w1, err := NewRotatingWriter(dir, "llm", RotateOptions{
		Daily: true, MaxSizeMB: 100, RetainDays: 14,
		RotateOnStart: true, LazyOpen: true,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRotatingWriter w1: %v", err)
	}

	// 如果没有 Write，则不生成/归档文件，不产生零字节文件
	_ = w1.Close()

	names, _ := listNames(dir)
	if len(names) != 1 || names[0] != "llm.log" {
		t.Fatalf("no writes occurred, expected only llm.log, got: %v", names)
	}
	content, _ := os.ReadFile(activePath)
	if string(content) != "prior session log\n" {
		t.Fatalf("content should be unchanged without write: %q", string(content))
	}

	// 2. 初始化 w2 并发起实际的 Write
	w2, err := NewRotatingWriter(dir, "llm", RotateOptions{
		Daily: true, MaxSizeMB: 100, RetainDays: 14,
		RotateOnStart: true, LazyOpen: true,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRotatingWriter w2: %v", err)
	}

	// 发起实际 Write，触发按次/启动归档
	if _, err := w2.Write([]byte("new session log line\n")); err != nil {
		t.Fatalf("write w2: %v", err)
	}
	_ = w2.Close()

	namesAfter, _ := listNames(dir)
	if len(namesAfter) != 2 {
		t.Fatalf("expected 2 files after write, got: %v", namesAfter)
	}
	if !contains(namesAfter, "llm.log") || !contains(namesAfter, "llm.2026-07-23.1.log") {
		t.Fatalf("unexpected archived file names: %v", namesAfter)
	}

	// 校验新旧文件内容
	oldContent, _ := os.ReadFile(filepath.Join(dir, "llm.2026-07-23.1.log"))
	if string(oldContent) != "prior session log\n" {
		t.Fatalf("old log content mismatch: %q", string(oldContent))
	}

	newContent, _ := os.ReadFile(activePath)
	if string(newContent) != "new session log line\n" {
		t.Fatalf("active llm.log content mismatch: %q", string(newContent))
	}
}

func listNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out, nil
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
