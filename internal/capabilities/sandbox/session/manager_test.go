package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
)

type managerTestClient struct {
	opens []sandboxcontract.WorkspaceRef
	raw   *managerTestRawSession
}

func (c *managerTestClient) OpenSession(_ context.Context, opts sandboxcontract.SessionOptions) (sandboxcontract.SandboxSession, error) {
	c.opens = append(c.opens, opts.Workspace)
	if opts.Workspace.ID == "missing-workspace" {
		return nil, fmt.Errorf("workspace not found")
	}
	if c.raw == nil {
		c.raw = &managerTestRawSession{}
	}
	return c.raw, nil
}

type managerTestRawSession struct {
	suspends int
	closes   int
}

func (*managerTestRawSession) Workspace() sandboxcontract.WorkspaceRef {
	return sandboxcontract.WorkspaceRef{ID: "live", Provider: "genesis-sandbox", Metadata: map[string]string{"workspace_id": "new-workspace"}}
}
func (*managerTestRawSession) Run(context.Context, sandboxcontract.CommandRequest) (*execmodel.Result, error) {
	return &execmodel.Result{ExitCode: 0}, nil
}
func (s *managerTestRawSession) Suspend(context.Context) error {
	s.suspends++
	return nil
}
func (s *managerTestRawSession) Close(context.Context) error {
	s.closes++
	return nil
}
func (*managerTestRawSession) ExpiresAt() time.Time { return time.Now().Add(time.Hour) }

type managerTestStore struct {
	loaded  sandboxcontract.WorkspaceRef
	deleted int
	saved   sandboxcontract.WorkspaceRef
	bound   int
}

func (s *managerTestStore) LoadExecutionSession(context.Context, string) (sandboxcontract.WorkspaceRef, bool, error) {
	return s.loaded, s.loaded.ID != "", nil
}
func (s *managerTestStore) SaveExecutionSession(_ context.Context, _ string, workspace sandboxcontract.WorkspaceRef) error {
	s.saved = workspace
	return nil
}
func (s *managerTestStore) DeleteExecutionSession(context.Context, string) error {
	s.deleted++
	return nil
}
func (s *managerTestStore) BindRemoteSession(context.Context, string, string, string, sandboxcontract.WorkspaceRef, time.Time) error {
	s.bound++
	return nil
}

func TestManagerRebuildsStaleDurableWorkspaceBeforeBusinessCommand(t *testing.T) {
	client := &managerTestClient{}
	store := &managerTestStore{loaded: sandboxcontract.WorkspaceRef{ID: "missing-workspace", Provider: "genesis-sandbox"}}
	manager, err := NewManager(ManagerDeps{Sessions: client, Files: &fakeFiles{}, Workspace: sandboxcontract.WorkspaceRef{ID: "workspace-root", Provider: "genesis-sandbox"}, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run", SessionID: "session", TaskID: "sandbox:command"}}
	workspace := execmodel.ExecutionWorkspace{WorkDir: "/workspace/work/binding", InputDir: "/workspace/input/binding", OutputDir: "/workspace/output/binding", TmpDir: "/workspace/tmp/binding"}
	handle, err := manager.Acquire(context.Background(), AcquireRequest{Binding: binding, Workspace: workspace, Sandbox: execmodel.SandboxProfile{Provider: "genesis-sandbox"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = handle.Close()
	if len(client.opens) != 2 || client.opens[0].ID != "missing-workspace" || client.opens[1].ID != "workspace-root" {
		t.Fatalf("open attempts=%+v", client.opens)
	}
	if store.deleted != 1 || store.saved.ID != "new-workspace" || store.bound != 1 {
		t.Fatalf("store=%+v", store)
	}
}

func TestManagerIdleCleanupSuspendsInsteadOfClose(t *testing.T) {
	raw := &managerTestRawSession{}
	client := &managerTestClient{raw: raw}
	store := &managerTestStore{}
	manager, err := NewManager(ManagerDeps{
		Sessions: client, Files: &fakeFiles{},
		Workspace:       sandboxcontract.WorkspaceRef{ID: "workspace-root", Provider: "genesis-sandbox"},
		Store:           store,
		IdleTTL:         20 * time.Millisecond,
		CacheTTL:        time.Hour,
		CleanupInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	binding := execmodel.ExecutionBinding{
		ID: "binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite,
		Owner: execmodel.ExecutionOwnerRef{RunID: "run", SessionID: "session", TaskID: "sandbox:command"},
	}
	workspace := execmodel.ExecutionWorkspace{
		WorkDir: "/workspace/work/binding", InputDir: "/workspace/input/binding",
		OutputDir: "/workspace/output/binding", TmpDir: "/workspace/tmp/binding",
	}
	handle, err := manager.Acquire(context.Background(), AcquireRequest{
		Binding: binding, Workspace: workspace, Sandbox: execmodel.SandboxProfile{Provider: "genesis-sandbox"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = handle.Close()
	// Run 级引用会阻止 idle 清理；释放后才允许 Suspend。
	manager.ReleaseRunID("run")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if raw.suspends >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if raw.suspends < 1 {
		t.Fatalf("expected Suspend on idle, suspends=%d closes=%d", raw.suspends, raw.closes)
	}
	// cacheTTL 很长：idle 清理不应 Close Session
	if raw.closes != 0 {
		t.Fatalf("idle cleanup must not Close session early, closes=%d", raw.closes)
	}
	manager.mu.Lock()
	alive := 0
	for _, entry := range manager.entries {
		if entry != nil && entry.session != nil {
			alive++
		}
	}
	manager.mu.Unlock()
	if alive != 1 {
		t.Fatalf("session should remain cached after Suspend, alive=%d", alive)
	}
}
