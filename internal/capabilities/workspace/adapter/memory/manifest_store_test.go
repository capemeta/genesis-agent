package memory

import (
	"context"
	"testing"
	"time"

	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestManifestStoreDoesNotExposeMutableInternalSlices(t *testing.T) {
	store := NewManifestStore()
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{AgentAppID: "app", AgentAppVersion: "1", RunID: "run"}}
	manifest := workmodel.RunManifest{SchemaVersion: "2", Revision: 1, RunID: "run", AgentApp: agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeProject, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}, StateRoot: workmodel.StateRoot{ID: "state", Authority: "test"}, Limits: workmodel.WorkspaceLimits{ProductModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject}, MaximumAccess: execmodel.WorkspaceAccessReadWrite}, Executions: []workmodel.PreparedExecutionSnapshot{{Binding: binding, Backend: testBackend(), Workspace: execmodel.ExecutionWorkspace{WorkDir: "/project", Metadata: map[string]string{"safe": "yes"}}}}, CreatedAt: time.Now().UTC()}
	if err := store.Create(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), "", "run")
	if err != nil {
		t.Fatal(err)
	}
	got.AgentApp.Workspace.AllowedModes[0] = execmodel.WorkspaceModeSession
	got.Executions[0].Workspace.Metadata["safe"] = "mutated"
	again, _ := store.Get(context.Background(), "", "run")
	if again.AgentApp.Workspace.AllowedModes[0] != execmodel.WorkspaceModeProject || again.Executions[0].Workspace.Metadata["safe"] != "yes" {
		t.Fatalf("store 内部状态被调用方修改: %+v", again)
	}
}

func TestManifestStoreRejectsCrossTenantLookup(t *testing.T) {
	store := NewManifestStore()
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant-a", AgentAppID: "app", AgentAppVersion: "1", RunID: "run"}}
	scope := workmodel.ResourceScope{TenantID: "tenant-a"}
	manifest := workmodel.RunManifest{SchemaVersion: "2", Revision: 1, RunID: "run", Scope: scope, AgentApp: agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeProject, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}, StateRoot: workmodel.StateRoot{ID: "state", Authority: "test", Scope: scope}, Limits: workmodel.WorkspaceLimits{MaximumAccess: execmodel.WorkspaceAccessReadWrite}, Executions: []workmodel.PreparedExecutionSnapshot{{Binding: binding, Backend: testBackend(), Workspace: execmodel.ExecutionWorkspace{WorkDir: "/project"}}}, CreatedAt: time.Now().UTC()}
	if err := store.Create(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), "tenant-b", "run"); err == nil {
		t.Fatal("cross-tenant lookup must fail")
	}
}

func TestManifestStoreRejectsStaleRevision(t *testing.T) {
	store := NewManifestStore()
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", AgentAppID: "app", AgentAppVersion: "1", RunID: "run"}}
	scope := workmodel.ResourceScope{TenantID: "tenant"}
	manifest := workmodel.RunManifest{SchemaVersion: "2", Revision: 1, RunID: "run", Scope: scope, AgentApp: agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeProject, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}, StateRoot: workmodel.StateRoot{ID: "state", Authority: "test", Scope: scope}, Limits: workmodel.WorkspaceLimits{MaximumAccess: execmodel.WorkspaceAccessReadWrite}, Executions: []workmodel.PreparedExecutionSnapshot{{Binding: binding, Backend: testBackend(), Workspace: execmodel.ExecutionWorkspace{WorkDir: "/project"}}}, CreatedAt: time.Now().UTC()}
	if err := store.Create(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	derived := execmodel.ExecutionBinding{ID: "derived", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", AgentAppID: "app", AgentAppVersion: "1", RunID: "run", TaskID: "task"}}
	if _, err := store.AddExecution(context.Background(), "tenant", "run", 0, workmodel.PreparedExecutionSnapshot{Binding: derived, Backend: testBackend(), Workspace: execmodel.ExecutionWorkspace{WorkDir: "/project"}}); err == nil {
		t.Fatal("stale revision update must fail")
	}
}

func testBackend() execmodel.ExecutionBackendRef {
	return execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Authority: "test"}
}
