package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	sandboxsession "genesis-agent/internal/capabilities/sandbox/session"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type commandTestRemote struct {
	files map[string][]byte
	runs  int
	last  sandboxcontract.CommandRequest
}

func (r *commandTestRemote) OpenSession(context.Context, sandboxcontract.SessionOptions) (sandboxcontract.SandboxSession, error) {
	return &commandTestSession{remote: r}, nil
}
func (r *commandTestRemote) ReadFile(context.Context, sandboxcontract.FileRequest, fscontract.ReadOptions) ([]byte, error) {
	return nil, nil
}
func (r *commandTestRemote) WriteFile(_ context.Context, req sandboxcontract.WriteFileRequest) error {
	r.files[cleanRemotePath(req.Path.WorkspaceRel)] = append([]byte(nil), req.Content...)
	return nil
}
func (r *commandTestRemote) ListDir(context.Context, sandboxcontract.ListDirRequest) ([]fsmodel.DirEntry, error) {
	return nil, nil
}
func (r *commandTestRemote) Walk(_ context.Context, req sandboxcontract.WalkRequest) (*fsmodel.WalkOutcome, error) {
	root := cleanRemotePath(req.Path.WorkspaceRel)
	entries := make([]fsmodel.DirEntry, 0)
	for name, content := range r.files {
		if strings.HasPrefix(name, root+"/") {
			entries = append(entries, fsmodel.DirEntry{Path: name, Name: name, Type: fsmodel.EntryTypeFile, Size: int64(len(content)), ModifiedAt: time.Unix(int64(len(content)), 0)})
		}
	}
	return &fsmodel.WalkOutcome{Root: root, Entries: entries}, nil
}
func (r *commandTestRemote) Stat(context.Context, sandboxcontract.FileRequest) (*fsmodel.FileStat, error) {
	return nil, nil
}
func (r *commandTestRemote) MkdirAll(context.Context, sandboxcontract.MkdirRequest) error { return nil }
func (r *commandTestRemote) Remove(context.Context, sandboxcontract.RemoveRequest) error  { return nil }

type commandTestSession struct{ remote *commandTestRemote }

func (s *commandTestSession) Workspace() sandboxcontract.WorkspaceRef {
	return sandboxcontract.WorkspaceRef{ID: "live-session", Provider: "genesis-sandbox", Metadata: map[string]string{"workspace_id": "durable-workspace"}}
}
func (s *commandTestSession) Run(_ context.Context, req sandboxcontract.CommandRequest) (*execmodel.Result, error) {
	s.remote.runs++
	s.remote.last = req
	s.remote.files["output/binding/result.txt"] = []byte("result")
	return &execmodel.Result{ExitCode: 0, Stdout: "ok"}, nil
}
func (*commandTestSession) Close(context.Context) error { return nil }
func (*commandTestSession) ExpiresAt() time.Time        { return time.Now().Add(time.Hour) }

type commandTestStore struct{ binds int }

func (commandTestStore) LoadExecutionSession(context.Context, string) (sandboxcontract.WorkspaceRef, bool, error) {
	return sandboxcontract.WorkspaceRef{}, false, nil
}
func (commandTestStore) SaveExecutionSession(context.Context, string, sandboxcontract.WorkspaceRef) error {
	return nil
}
func (commandTestStore) DeleteExecutionSession(context.Context, string) error { return nil }
func (s *commandTestStore) BindRemoteSession(context.Context, string, string, string, sandboxcontract.WorkspaceRef, time.Time) error {
	s.binds++
	return nil
}

type commandTestInputs struct{ content []byte }

func (s commandTestInputs) OpenSnapshot(context.Context, workmodel.WorkspacePath) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.content)), nil
}

type commandTestRegistrar struct {
	requests []workcontract.RegisterProducedResourceRequest
}

func (r *commandTestRegistrar) RegisterProducedResource(_ context.Context, req workcontract.RegisterProducedResourceRequest) (workmodel.ProducedResourceDescriptor, error) {
	r.requests = append(r.requests, req)
	return workmodel.ProducedResourceDescriptor{ID: fmt.Sprintf("resource-%d", len(r.requests)), LogicalRef: req.LogicalRef, ObservedName: req.ObservedName, MediaType: req.MediaType, Size: req.Size}, nil
}

func TestSandboxCommandStagesInputsAndRegistersOnlyOutputDir(t *testing.T) {
	remote := &commandTestRemote{files: map[string][]byte{"work/binding/old.tmp": []byte("old")}}
	store := &commandTestStore{}
	manager, err := sandboxsession.NewManager(sandboxsession.ManagerDeps{
		Sessions: remote, Files: remote, Workspace: sandboxcontract.WorkspaceRef{ID: "root", Provider: "genesis-sandbox"}, Store: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	content := []byte("source")
	digest := sha256.Sum256(content)
	registrar := &commandTestRegistrar{}
	service, err := NewSandboxCommandService(manager, commandTestInputs{content: content}, registrar)
	if err != nil {
		t.Fatal(err)
	}
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyStrictWorkspace, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", UserID: "user", SessionID: "session", RunID: "run", TaskID: "sandbox:command"}}
	workspace := execmodel.ExecutionWorkspace{WorkDir: "/workspace/work/binding", InputDir: "/workspace/input/binding", OutputDir: "/workspace/output/binding", TmpDir: "/workspace/tmp/binding"}
	result, err := service.RunSandboxCommand(context.Background(), SandboxCommandRequest{
		Command: execmodel.Command{Command: "build"}, Binding: binding, Workspace: workspace,
		Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox", RuntimeProfile: execmodel.RuntimeProfileCodePolyglotBasic},
		Inputs:  workmodel.InputManifest{RunID: "run", BindingID: "binding", Inputs: []workmodel.InputRef{{ID: "input", Alias: "source.txt", Size: int64(len(content)), SHA256: fmt.Sprintf("%x", digest), StagedPath: "snapshots/source.txt"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if remote.runs != 1 || remote.last.Command.Cwd != workspace.WorkDir || remote.last.Command.Env["OUTPUT_DIR"] != workspace.OutputDir {
		t.Fatalf("run request = %+v", remote.last)
	}
	if got := string(remote.files["work/binding/source.txt"]); got != "source" {
		t.Fatalf("staged input = %q", got)
	}
	if len(result.Produced) != 1 || len(registrar.requests) != 1 || registrar.requests[0].ObservedPath != "output/binding/result.txt" {
		t.Fatalf("produced=%+v registrations=%+v", result.Produced, registrar.requests)
	}
	if store.binds < 2 {
		t.Fatalf("binding lease should be refreshed before registration: binds=%d", store.binds)
	}
	second, err := service.RunSandboxCommand(context.Background(), SandboxCommandRequest{
		Command: execmodel.Command{Command: "build-again"}, Binding: binding, Workspace: workspace,
		Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Produced) != 1 || len(registrar.requests) != 2 {
		t.Fatalf("existing output must remain a registered candidate: produced=%+v registrations=%d", second.Produced, len(registrar.requests))
	}
}

func TestSandboxCommandRejectsTamperedInputBeforeExecution(t *testing.T) {
	remote := &commandTestRemote{files: map[string][]byte{}}
	store := &commandTestStore{}
	manager, _ := sandboxsession.NewManager(sandboxsession.ManagerDeps{Sessions: remote, Files: remote, Workspace: sandboxcontract.WorkspaceRef{ID: "root"}, Store: store})
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	service, _ := NewSandboxCommandService(manager, commandTestInputs{content: []byte("changed")}, &commandTestRegistrar{})
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run"}}
	workspace := execmodel.ExecutionWorkspace{WorkDir: "/workspace/work/binding", InputDir: "/workspace/input/binding", OutputDir: "/workspace/output/binding", TmpDir: "/workspace/tmp/binding"}
	_, err := service.RunSandboxCommand(context.Background(), SandboxCommandRequest{Command: execmodel.Command{Command: "build"}, Binding: binding, Workspace: workspace, Sandbox: execmodel.SandboxProfile{Provider: "genesis-sandbox"}, Inputs: workmodel.InputManifest{RunID: "run", BindingID: "binding", Inputs: []workmodel.InputRef{{ID: "input", Alias: "source.txt", Size: 7, SHA256: strings.Repeat("0", 64), StagedPath: "snapshots/source.txt"}}}})
	if err == nil || remote.runs != 0 {
		t.Fatalf("tampered input should fail before command: runs=%d err=%v", remote.runs, err)
	}
}

func cleanRemotePath(value string) string {
	return strings.TrimPrefix(strings.ReplaceAll(value, `\`, "/"), "/workspace/")
}
