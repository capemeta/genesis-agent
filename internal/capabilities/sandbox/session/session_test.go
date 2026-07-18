package session

import (
	"context"
	"strings"
	"testing"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
)

type fakeSessionClient struct {
	opened sandboxcontract.SessionOptions
	raw    *fakeRawSession
}

func (c *fakeSessionClient) OpenSession(ctx context.Context, opts sandboxcontract.SessionOptions) (sandboxcontract.SandboxSession, error) {
	c.opened = opts
	if c.raw == nil {
		c.raw = &fakeRawSession{}
	}
	return c.raw, nil
}

type fakeRawSession struct {
	workspace sandboxcontract.WorkspaceRef
	lastRun   sandboxcontract.CommandRequest
	closed    int
}

func (s *fakeRawSession) Workspace() sandboxcontract.WorkspaceRef {
	if s.workspace.ID == "" {
		return sandboxcontract.WorkspaceRef{ID: "session-1", Provider: "genesis-sandbox", Metadata: map[string]string{"session_id": "session-1"}}
	}
	return s.workspace
}

func (s *fakeRawSession) Run(ctx context.Context, req sandboxcontract.CommandRequest) (*execmodel.Result, error) {
	s.lastRun = req
	return &execmodel.Result{ExitCode: 0, Stdout: "ok"}, nil
}

func (s *fakeRawSession) Close(context.Context) error {
	s.closed++
	return nil
}

type fakeFiles struct {
	lastWrite sandboxcontract.WriteFileRequest
	lastRead  sandboxcontract.FileRequest
}

func (f *fakeFiles) ReadFile(ctx context.Context, req sandboxcontract.FileRequest, opts fscontract.ReadOptions) ([]byte, error) {
	f.lastRead = req
	return []byte("data"), nil
}

func (f *fakeFiles) WriteFile(ctx context.Context, req sandboxcontract.WriteFileRequest) error {
	f.lastWrite = req
	return nil
}

func (f *fakeFiles) ListDir(context.Context, sandboxcontract.ListDirRequest) ([]fsmodel.DirEntry, error) {
	return nil, nil
}

func (f *fakeFiles) Walk(context.Context, sandboxcontract.WalkRequest) (*fsmodel.WalkOutcome, error) {
	return &fsmodel.WalkOutcome{}, nil
}

func (f *fakeFiles) Stat(context.Context, sandboxcontract.FileRequest) (*fsmodel.FileStat, error) {
	return &fsmodel.FileStat{}, nil
}

func (f *fakeFiles) MkdirAll(context.Context, sandboxcontract.MkdirRequest) error {
	return nil
}

func (f *fakeFiles) Remove(context.Context, sandboxcontract.RemoveRequest) error {
	return nil
}

func TestOpenRunAndWorkspaceFSUseSameSessionWorkspace(t *testing.T) {
	sessions := &fakeSessionClient{}
	files := &fakeFiles{}
	sb, err := Open(context.Background(), Deps{Sessions: sessions, Files: files}, Options{
		Workspace: sandboxcontract.WorkspaceRef{ID: "ws-1"},
		Sandbox: execmodel.SandboxProfile{
			RuntimeProfile: execmodel.RuntimeProfileOfficeBasic,
			TaskType:       execmodel.SandboxTaskOffice,
			Operation:      execmodel.SandboxOperationRunSkill,
			Language:       "python",
		},
		Run: execcontract.RunOptions{
			Timeout: 2 * time.Minute,
			Binding: execmodel.ExecutionBinding{
				ID: "binding-session-1", Mode: execmodel.WorkspaceModeSession, Access: execmodel.WorkspaceAccessReadWrite,
				Owner: execmodel.ExecutionOwnerRef{RunID: "run-session-1"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessions.opened.Workspace.ID != "ws-1" || sessions.opened.Sandbox.RuntimeProfile != execmodel.RuntimeProfileOfficeBasic {
		t.Fatalf("opened=%+v", sessions.opened)
	}
	result, err := sb.RunCommand(context.Background(), "python scripts/check.py", execcontract.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "ok" {
		t.Fatalf("result=%+v", result)
	}
	if sessions.raw.lastRun.Workspace.ID != "session-1" || sessions.raw.lastRun.Command.Cwd != "/workspace" {
		t.Fatalf("run=%+v", sessions.raw.lastRun)
	}
	if err := sb.WriteFile(context.Background(), "scripts/check.py", []byte("print('ok')"), fscontract.WriteOptions{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if files.lastWrite.Workspace.ID != "session-1" || files.lastWrite.Path.WorkspaceRel != "scripts/check.py" {
		t.Fatalf("write=%+v", files.lastWrite)
	}
	data, err := sb.ReadFile(context.Background(), "output.pptx", fscontract.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "data" || files.lastRead.Workspace.ID != "session-1" || files.lastRead.Path.WorkspaceRel != "output.pptx" {
		t.Fatalf("read data=%q req=%+v", string(data), files.lastRead)
	}
	if err := sb.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := sb.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sessions.raw.closed != 1 {
		t.Fatalf("closed=%d", sessions.raw.closed)
	}
}

func TestDefaultOptionsDoNotChooseSandboxProvider(t *testing.T) {
	opts := DefaultOptions()
	if opts.Sandbox.Provider != "" || opts.Sandbox.Mode != "" || opts.Sandbox.RuntimeProfile != "" {
		t.Fatalf("default sandbox should be product-neutral: %+v", opts.Sandbox)
	}
	if opts.Run.Workspace.WorkDir != "/workspace" || opts.Run.Workspace.OutputDir != "/workspace/output" {
		t.Fatalf("default workspace=%+v", opts.Run.Workspace)
	}
}

func TestRunOptionsMergeWorkspaceByField(t *testing.T) {
	base := DefaultOptions().Run
	merged := mergeRunOptions(base, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{
			SkillDir: "/workspace/skills/office-ppt",
		},
	})
	if merged.Workspace.WorkDir != "/workspace" || merged.Workspace.OutputDir != "/workspace/output" {
		t.Fatalf("default dirs were lost: %+v", merged.Workspace)
	}
	if merged.Workspace.SkillDir != "/workspace/skills/office-ppt" {
		t.Fatalf("skill dir not merged: %+v", merged.Workspace)
	}
}
func TestOpenRequiresSessionAndFilePorts(t *testing.T) {
	_, err := Open(context.Background(), Deps{}, Options{})
	if err == nil || !strings.Contains(err.Error(), "session client") {
		t.Fatalf("err=%v", err)
	}
	_, err = Open(context.Background(), Deps{Sessions: &fakeSessionClient{}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "filesystem client") {
		t.Fatalf("err=%v", err)
	}
}
