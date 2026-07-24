package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"genesis-agent/internal/platform/config"
)

func TestNewRuntimeLogging_CreatesFourChannels(t *testing.T) {
	dir := t.TempDir()
	enabled := true
	cfg := config.LogConfig{
		Level: "info",
		Dir:   dir,
		Rotate: config.LogRotateConfig{
			MaxSizeMB:  100,
			RetainDays: 14,
		},
		Channels: map[string]config.LogChannelConfig{
			"agent": {Enabled: &enabled, File: "agent.log", Format: "text", RetainDays: 14},
			"audit": {Enabled: &enabled, File: "audit.log", Format: "jsonl", RetainDays: 90},
			"usage": {Enabled: &enabled, File: "usage.log", Format: "jsonl", RetainDays: 90},
			"llm":   {Enabled: &enabled, File: "llm.log", Format: "jsonl", RetainDays: 14},
		},
	}

	rt, err := NewRuntimeLogging(cfg, RuntimeLoggingOptions{ConfigDir: filepath.Join(dir, "configs"), Quiet: true})
	if err != nil {
		t.Fatalf("NewRuntimeLogging: %v", err)
	}
	defer rt.Close()

	rt.AgentLogger.Info("hello", "run_id", "r1")
	if rt.AuditWriter == nil || rt.UsageWriter == nil || rt.LLMWriter == nil {
		t.Fatal("audit/usage/llm writers should be enabled")
	}
	if _, err := rt.AuditWriter.Write([]byte("{\"type\":\"test\"}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.UsageWriter.Write([]byte("{\"type\":\"tool.usage\"}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.LLMWriter.Write([]byte("{\"model\":\"test\"}\n")); err != nil {
		t.Fatal(err)
	}
	_ = rt.Close()

	for _, name := range []string{"agent.log", "audit.log", "usage.log", "llm.log"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "agent.log"))
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("agent.log = %q", data)
	}
}
