package service

import (
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
)

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

func TestAttachExecutionPathContextOmitsPhysicalPathMap(t *testing.T) {
	out := &scriptcontract.RunResult{Metadata: map[string]string{}}
	attachExecutionPathContext(out, executionBackendRemoteSandbox, false)
	if out.Metadata[metaExecutionBackend] != executionBackendRemoteSandbox {
		t.Fatalf("execution_backend=%q", out.Metadata[metaExecutionBackend])
	}
	if out.Metadata[metaDegraded] != "false" {
		t.Fatalf("degraded=%q", out.Metadata[metaDegraded])
	}
	if out.Metadata[metaBackendLegacy] != "remote_session" {
		t.Fatalf("backend=%q", out.Metadata[metaBackendLegacy])
	}
	if _, ok := out.Metadata["path_map"]; ok {
		t.Fatalf("path_map must not be exposed to model: %v", out.Metadata)
	}
	if _, ok := out.Metadata["path_map_note"]; ok {
		t.Fatal("path_map_note must not be exposed to model")
	}

	out2 := &scriptcontract.RunResult{}
	attachExecutionPathContext(out2, executionBackendLocalHost, true)
	if out2.Metadata[metaDegraded] != "true" {
		t.Fatalf("degraded=%q", out2.Metadata[metaDegraded])
	}
	if out2.Metadata[metaBackendLegacy] != "local" {
		t.Fatalf("legacy backend after degrade=%q", out2.Metadata[metaBackendLegacy])
	}
	if _, ok := out2.Metadata["path_map"]; ok {
		t.Fatal("degraded path_map must not be exposed")
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

func testRemoteSkillWorkspace() execmodel.ExecutionWorkspace {
	return remoteSkillWorkspace(execmodel.ExecutionBinding{ID: "binding-1"}, skillmodel.Metadata{PackageID: "office-ppt"})
}
