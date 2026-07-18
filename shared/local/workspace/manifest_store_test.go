package workspace

import (
	"context"
	"os"
	"testing"
	"time"

	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestManifestStoreAppendsImmutableRevisionAndSkipsCorruptNewerFile(t *testing.T) {
	root := t.TempDir()
	store, err := NewManifestStore(root)
	if err != nil {
		t.Fatal(err)
	}
	profile := agentappmodel.EffectiveProfile{ID: "app", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeProject, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}}
	rootBinding := execmodel.ExecutionBinding{ID: "root-binding", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyPermissionOnly, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", AgentAppID: "app", AgentAppVersion: "1", RunID: "run-1"}}
	scope := workmodel.ResourceScope{TenantID: "tenant"}
	backend := execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindHost, Provider: "local-host", Authority: "host"}
	manifest := workmodel.RunManifest{SchemaVersion: workmodel.RunManifestSchemaVersion, Revision: 1, RunID: "run-1", Scope: scope, AgentApp: profile, StateRoot: workmodel.StateRoot{ID: "state", Authority: "host", Path: root, Scope: scope}, Limits: workmodel.WorkspaceLimits{MaximumAccess: execmodel.WorkspaceAccessReadWrite}, Executions: []workmodel.PreparedExecutionSnapshot{{Binding: rootBinding, Backend: backend, Workspace: execmodel.ExecutionWorkspace{WorkDir: root}}}, Inputs: workmodel.InputManifest{RunID: "run-1", BindingID: "root-binding"}, View: workmodel.WorkspaceViewManifest{BindingID: "root-binding", Root: "."}, CreatedAt: time.Now().UTC()}
	if err := store.Create(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	derivedBinding := execmodel.ExecutionBinding{ID: "derived-binding", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyPermissionOnly, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", AgentAppID: "app", AgentAppVersion: "1", RunID: "run-1", TaskID: "hook:guard"}}
	updated, err := store.AddExecution(context.Background(), "tenant", "run-1", 1, workmodel.PreparedExecutionSnapshot{Binding: derivedBinding, Backend: backend, Workspace: execmodel.ExecutionWorkspace{WorkDir: root}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || len(updated.Executions) != 2 {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := store.AddExecution(context.Background(), "tenant", "run-1", 1, workmodel.PreparedExecutionSnapshot{Binding: derivedBinding, Backend: backend, Workspace: execmodel.ExecutionWorkspace{WorkDir: root}}); err == nil {
		t.Fatal("stale revision update must fail")
	}
	corrupt := store.revisionFilename("tenant", "run-1", 3)
	if err := os.WriteFile(corrupt, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Get(context.Background(), "tenant", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Revision != 2 {
		t.Fatalf("recovered revision = %d", recovered.Revision)
	}
	if _, err := store.Get(context.Background(), "other-tenant", "run-1"); err == nil {
		t.Fatal("cross-tenant lookup must fail")
	}
}
