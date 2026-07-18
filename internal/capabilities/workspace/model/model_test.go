package model

import (
	"testing"
	"time"

	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func TestWorkspacePathValidate(t *testing.T) {
	for _, valid := range []WorkspacePath{"report.md", "nested/report.pptx"} {
		if err := valid.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v", valid, err)
		}
	}
	for _, invalid := range []WorkspacePath{"", "../secret", "a/../secret", "/root/file", `C:\secret.txt`, `nested\file`} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("Validate(%q) error = nil", invalid)
		}
	}
}

func TestRunManifestValidateRejectsScopeMismatch(t *testing.T) {
	scope := ResourceScope{TenantID: "tenant-a", ProjectID: "project-a", UserID: "user-a"}
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{TenantID: scope.TenantID, ProjectID: scope.ProjectID, UserID: scope.UserID, AgentAppID: "app", AgentAppVersion: "1", RunID: "run"}}
	workspace := execmodel.ExecutionWorkspace{WorkDir: "/workspace/work", InputDir: "/workspace/input", OutputDir: "/workspace/output", TmpDir: "/workspace/tmp"}
	manifest := RunManifest{SchemaVersion: RunManifestSchemaVersion, Revision: 1, RunID: "run", Scope: scope, AgentApp: agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}, StateRoot: StateRoot{ID: "state", Authority: "test", Scope: scope}, Limits: WorkspaceLimits{MaximumAccess: execmodel.WorkspaceAccessReadWrite}, Executions: []PreparedExecutionSnapshot{{Binding: binding, Backend: execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Authority: "test"}, Workspace: workspace}}, Inputs: InputManifest{RunID: "run", BindingID: "binding"}, View: WorkspaceViewManifest{BindingID: "binding", Root: "."}, CreatedAt: time.Now().UTC()}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	wrongState := manifest
	wrongState.StateRoot.Scope.TenantID = "tenant-b"
	if err := wrongState.Validate(); err == nil {
		t.Fatal("state root scope mismatch must fail")
	}

	wrongProject := manifest
	wrongProject.ProjectRoot = &ResourceRef{Authority: "host", Scheme: "project", ID: "project", Scope: ResourceScope{TenantID: "tenant-b"}}
	if err := wrongProject.Validate(); err == nil {
		t.Fatal("project root scope mismatch must fail")
	}

	wrongOwner := manifest
	wrongOwner.Executions = append([]PreparedExecutionSnapshot(nil), manifest.Executions...)
	wrongOwner.Executions[0].Binding.Owner.TenantID = "tenant-b"
	if err := wrongOwner.Validate(); err == nil {
		t.Fatal("execution owner scope mismatch must fail")
	}
}
