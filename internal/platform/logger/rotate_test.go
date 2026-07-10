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
