package file_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	auditfile "genesis-agent/internal/capabilities/audit/adapter/file"
	"genesis-agent/internal/capabilities/audit/model"
	usagefile "genesis-agent/internal/capabilities/usage/adapter/file"
	usagemodel "genesis-agent/internal/capabilities/usage/model"
	"genesis-agent/internal/platform/contextutil"
)

func TestAuditFileSink_JSONLAndRedact(t *testing.T) {
	var buf bytes.Buffer
	sink := auditfile.NewSink(&buf)
	err := sink.Record(context.Background(), model.Event{
		Category: "approval.decision",
		Action:   "skill.load",
		Resource: "Skill(office-ppt)",
		Severity: model.SeverityInfo,
		Allowed:  true,
		Metadata: map[string]string{"api_key": "secret-value", "run_id": "r1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(buf.String())
	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatalf("json: %v body=%q", err, line)
	}
	if payload["type"] != "approval.decision" {
		t.Fatalf("type = %v", payload["type"])
	}
	if payload["run_id"] != "r1" {
		t.Fatalf("top-level run_id = %v", payload["run_id"])
	}
	meta, _ := payload["metadata"].(map[string]any)
	if meta["api_key"] != "[redacted]" {
		t.Fatalf("api_key not redacted: %v", meta)
	}
}

func TestAuditFileSink_FromContext(t *testing.T) {
	var buf bytes.Buffer
	sink := auditfile.NewSink(&buf)
	ctx := contextutil.WithRunID(context.Background(), "run-ctx")
	ctx = contextutil.WithSessionID(ctx, "sess-1")
	if err := sink.Record(ctx, model.Event{
		Category: "tool",
		Action:   "read_file.finish",
		Resource: "read_file",
		Severity: model.SeverityInfo,
		Allowed:  true,
	}); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["run_id"] != "run-ctx" || payload["session_id"] != "sess-1" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestUsageFileSink_JSONL(t *testing.T) {
	var buf bytes.Buffer
	sink := usagefile.NewSink(&buf)
	now := time.Now()
	ctx := contextutil.WithRunID(context.Background(), "run-u1")
	err := sink.RecordToolUsage(ctx, usagemodel.ToolUsage{
		ToolName:    "Skill",
		Success:     true,
		DurationMS:  12,
		StartedAt:   now,
		CompletedAt: now,
		Metadata:    map[string]string{"token": "abc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != "tool.usage" || payload["tool_name"] != "Skill" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["run_id"] != "run-u1" {
		t.Fatalf("run_id = %v", payload["run_id"])
	}
	meta, _ := payload["metadata"].(map[string]any)
	if meta["token"] != "[redacted]" {
		t.Fatalf("token not redacted: %v", meta)
	}
}
