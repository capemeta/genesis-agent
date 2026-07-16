package service

import (
	"strings"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
)

func TestIsExecutionPlaneAbsoluteInput(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"/workspace/foo.py", true},
		{"/workspace", true},
		{`/workspace\.genesis\runs\r1\work\a.py`, true},
		{"$WORK_DIR/foo.py", false},
		{"foo.py", false},
		{".genesis/runs/r1/work/foo.py", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isExecutionPlaneAbsoluteInput(tc.in); got != tc.want {
			t.Fatalf("isExecutionPlaneAbsoluteInput(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestResolveExecutionBackendDegradedUsesLocal(t *testing.T) {
	sandbox := execmodel.SandboxProfile{Provider: "genesis-sandbox"}
	got := resolveExecutionBackend(sandbox, true, true)
	if got != executionBackendLocalHost {
		t.Fatalf("got=%s", got)
	}
	got = resolveExecutionBackend(sandbox, true, false)
	if got != executionBackendRemoteSandbox {
		t.Fatalf("got=%s", got)
	}
	got = resolveExecutionBackend(execmodel.SandboxProfile{Provider: "local-platform"}, false, false)
	if got != executionBackendLocalPlatformSandbox {
		t.Fatalf("got=%s", got)
	}
}

func TestAttachExecutionPathContextIncludesMapOnChange(t *testing.T) {
	s := &Service{pathHints: map[string]pathHintState{}}
	out := &scriptcontract.RunResult{Metadata: map[string]string{}}
	key := formatPathContextKey("run-1", "office-pdf")
	include := s.shouldIncludePathMap(key, executionBackendRemoteSandbox, false)
	if !include {
		t.Fatal("first call should include path_map")
	}
	// 第二次同 key 更新 lastUsed；状态已写入
	if _, ok := s.pathHints[key]; !ok {
		t.Fatal("pathHints missing after first include")
	}
	attachExecutionPathContext(out, executionBackendRemoteSandbox, false, include, "run-1", "office-pdf")
	if out.Metadata[metaExecutionBackend] != executionBackendRemoteSandbox {
		t.Fatalf("execution_backend=%q", out.Metadata[metaExecutionBackend])
	}
	if out.Metadata[metaDegraded] != "false" {
		t.Fatalf("degraded=%q", out.Metadata[metaDegraded])
	}
	if out.Metadata[metaBackendLegacy] != "remote_session" {
		t.Fatalf("backend=%q", out.Metadata[metaBackendLegacy])
	}
	if !strings.Contains(out.Metadata[metaPathMap], "/workspace") {
		t.Fatalf("path_map=%q", out.Metadata[metaPathMap])
	}
	if out.Metadata[metaPathMapNote] == "" {
		t.Fatal("missing path_map_note")
	}
	if note := out.Metadata[metaPathMapNote]; !strings.Contains(note, "$WORK_DIR") || !strings.Contains(note, "$OUTPUT_DIR") || !strings.Contains(note, "最终文本交付物") {
		t.Fatalf("path_map_note must distinguish intermediate and final files: %q", note)
	}

	include2 := s.shouldIncludePathMap(key, executionBackendRemoteSandbox, false)
	if include2 {
		t.Fatal("second identical call should omit path_map")
	}
	out2 := &scriptcontract.RunResult{Metadata: map[string]string{}}
	attachExecutionPathContext(out2, executionBackendRemoteSandbox, false, include2, "run-1", "office-pdf")
	if out2.Metadata[metaPathMap] != "" {
		t.Fatalf("unexpected path_map on stable call: %q", out2.Metadata[metaPathMap])
	}
	if out2.Metadata[metaExecutionBackend] != executionBackendRemoteSandbox {
		t.Fatal("execution_backend must still be set")
	}

	include3 := s.shouldIncludePathMap(key, executionBackendLocalHost, true)
	if !include3 {
		t.Fatal("degraded backend change should include path_map")
	}
	out3 := &scriptcontract.RunResult{}
	attachExecutionPathContext(out3, executionBackendLocalHost, true, include3, "run-1", "office-pdf")
	if out3.Metadata[metaDegraded] != "true" {
		t.Fatalf("degraded=%q", out3.Metadata[metaDegraded])
	}
	if out3.Metadata[metaBackendLegacy] != "local" {
		t.Fatalf("legacy backend after degrade=%q", out3.Metadata[metaBackendLegacy])
	}
	if !strings.Contains(out3.Metadata[metaPathMap], ".genesis/runs/run-1") {
		t.Fatalf("local path_map=%q", out3.Metadata[metaPathMap])
	}
	if len(out3.Warnings) == 0 || !strings.Contains(out3.Warnings[0], "降级刷新") {
		t.Fatalf("warnings=%v", out3.Warnings)
	}
}

func TestDetectDegradedFromWarnings(t *testing.T) {
	if !detectDegradedFromWarnings([]string{"genesis-sandbox session打开失败，sandbox optional 已降级到本地执行: boom"}) {
		t.Fatal("expected degraded")
	}
	if detectDegradedFromWarnings([]string{"ok"}) {
		t.Fatal("not degraded")
	}
}
