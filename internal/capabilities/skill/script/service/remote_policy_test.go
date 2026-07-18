package service

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	sandboxsession "genesis-agent/internal/capabilities/sandbox/session"
	"genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type fakeRemoteClient struct {
	files       map[string][]byte
	lastCommand execmodel.Command
	lastOptions sandboxcontract.SessionOptions
	openCount   int
	runCount    int
	closeCount  int
}

func newFakeRemoteClient() *fakeRemoteClient { return &fakeRemoteClient{files: map[string][]byte{}} }
func (c *fakeRemoteClient) OpenSession(_ context.Context, opts sandboxcontract.SessionOptions) (sandboxcontract.SandboxSession, error) {
	c.openCount++
	c.lastOptions = opts
	return &fakeRemoteSession{client: c}, nil
}
func (c *fakeRemoteClient) ReadFile(_ context.Context, req sandboxcontract.FileRequest, _ fscontract.ReadOptions) ([]byte, error) {
	return append([]byte(nil), c.files[normalizeSlash(req.Path.WorkspaceRel)]...), nil
}
func (c *fakeRemoteClient) WriteFile(_ context.Context, req sandboxcontract.WriteFileRequest) error {
	path := normalizeSlash(req.Path.WorkspaceRel)
	if _, exists := c.files[path]; exists && !req.Options.Overwrite {
		return fscontract.NewError(fscontract.ErrCodeAlreadyExists, path, fmt.Errorf("目标文件已存在"))
	}
	c.files[path] = append([]byte(nil), req.Content...)
	return nil
}
func (c *fakeRemoteClient) ListDir(context.Context, sandboxcontract.ListDirRequest) ([]fsmodel.DirEntry, error) {
	return nil, nil
}
func (c *fakeRemoteClient) Walk(_ context.Context, req sandboxcontract.WalkRequest) (*fsmodel.WalkOutcome, error) {
	entries := make([]fsmodel.DirEntry, 0, len(c.files))
	modifiedAt := time.Unix(1, 0)
	root := normalizeSlash(req.Path.WorkspaceRel)
	for filePath, data := range c.files {
		if filePath != root && !strings.HasPrefix(filePath, root+"/") {
			continue
		}
		entries = append(entries, fsmodel.DirEntry{Name: filePath, Path: filePath, Type: fsmodel.EntryTypeFile, Size: int64(len(data)), ModifiedAt: modifiedAt})
	}
	return &fsmodel.WalkOutcome{Root: req.Path.WorkspaceRel, Entries: entries}, nil
}
func (c *fakeRemoteClient) Stat(_ context.Context, req sandboxcontract.FileRequest) (*fsmodel.FileStat, error) {
	path := normalizeSlash(req.Path.WorkspaceRel)
	data, ok := c.files[path]
	if !ok {
		return nil, nil
	}
	// 刻意与 Walk 的时间戳不同，证明候选发现元数据不被误用为版本身份。
	return &fsmodel.FileStat{Path: fsmodel.ResolvedPath{WorkspaceRel: path}, Type: fsmodel.EntryTypeFile, Size: int64(len(data)), ModifiedAt: time.Unix(2, 0)}, nil
}
func (c *fakeRemoteClient) MkdirAll(context.Context, sandboxcontract.MkdirRequest) error { return nil }
func (c *fakeRemoteClient) Remove(context.Context, sandboxcontract.RemoveRequest) error  { return nil }

type fakeRemoteSession struct{ client *fakeRemoteClient }

func (s *fakeRemoteSession) Workspace() sandboxcontract.WorkspaceRef {
	return sandboxcontract.WorkspaceRef{ID: "session-1", Provider: "genesis-sandbox", Metadata: map[string]string{"session_id": "session-1", "workspace_id": "workspace-1", "sandbox_id": "sandbox-1"}}
}
func (s *fakeRemoteSession) Run(_ context.Context, req sandboxcontract.CommandRequest) (*execmodel.Result, error) {
	s.client.lastCommand = req.Command
	s.client.runCount++
	s.client.files[path.Join(normalizeSlash(req.Command.Cwd), "output.txt")] = []byte("done")
	return &execmodel.Result{ExitCode: 0, Stdout: "ok"}, nil
}
func (s *fakeRemoteSession) Close(context.Context) error {
	s.client.closeCount++
	return nil
}
func (s *fakeRemoteSession) ExpiresAt() time.Time { return time.Now().Add(time.Hour) }

func TestSkillCommandServiceRunsInRemoteTaskWorkspace(t *testing.T) {
	client := newFakeRemoteClient()
	source, err := embedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, skillmodel.ScopeSystem, fstest.MapFS{
		"demo/SKILL.md":                {Data: []byte("---\nname: demo\ndescription: demo skill\nallowed-tools:\n  - run_skill_command\ndependencies:\n  runtime:\n    system:\n      - name: libreoffice\n        command: soffice\n---\nDemo")},
		"demo/scripts/make_output.cmd": {Data: []byte("@echo off\r\necho remote>output.txt\r\n")},
	}, skillparser.New())
	if err != nil {
		t.Fatal(err)
	}
	skills := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	registrar := &collectingProducedRegistrar{}
	remoteStore := noOpRemoteSessionBinder{}
	manager, err := sandboxsession.NewManager(sandboxsession.ManagerDeps{
		Sessions: client, Files: client, Workspace: sandboxcontract.WorkspaceRef{ID: "w1", Provider: "genesis-sandbox"},
		Store: remoteStore, IdleTTL: time.Millisecond, CacheTTL: time.Hour, CleanupInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	svc, err := New(Deps{Skills: skills, Runner: nilRunner{}, Approval: allowAllApproval{}, SessionClient: client, FileClient: client, WorkspaceRef: sandboxcontract.WorkspaceRef{ID: "w1", Provider: "genesis-sandbox"}, Provisioner: testProvisioner{}, ProducedResources: registrar, RemoteSessions: remoteStore, SessionManager: manager})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	root := t.TempDir()
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{Catalog: skillcontract.CatalogRequest{}, Skill: "demo", Command: `./scripts/make_output.cmd`, Binding: testBinding("remote-run"), StateRoot: testStateRoot(root), ProjectDir: root, Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result=%+v", result)
	}
	if client.lastCommand.Cwd != "/workspace/work/remote-run-skill-demo/skills/demo" {
		t.Fatalf("cwd=%q", client.lastCommand.Cwd)
	}
	if client.lastCommand.Env["INPUT_DIR"] != "/workspace/input/remote-run-skill-demo" || client.lastCommand.Env["OUTPUT_DIR"] != "/workspace/output/remote-run-skill-demo" || client.lastCommand.Env["TMPDIR"] != "/workspace/tmp/remote-run-skill-demo" {
		t.Fatalf("remote task env=%v", client.lastCommand.Env)
	}
	if !strings.Contains(client.lastCommand.Command, "scripts") {
		t.Fatalf("command=%q", client.lastCommand.Command)
	}
	if _, ok := client.files["work/remote-run-skill-demo/skills/demo/scripts/make_output.cmd"]; !ok {
		t.Fatalf("materialized files=%v", client.files)
	}
	if !containsProduced(result.Produced, "output.txt") {
		t.Fatalf("produced=%v", result.Produced)
	}
	if len(registrar.registrations) != 1 || result.Produced[0].CandidateID == "" {
		t.Fatalf("produced registrations=%+v", registrar.registrations)
	}
	matDir := filepath.Join(root, ".genesis", "runtime", "runs", "remote-run-materialize")
	if _, err := os.Stat(matDir); !os.IsNotExist(err) {
		t.Fatalf("should not create separate -materialize run dir: %v", err)
	}
	svc.ReleaseRun(context.Background(), workmodel.PreparedRun{Manifest: workmodel.RunManifest{RunID: "remote-run"}})
	if client.closeCount != 0 {
		t.Fatalf("Run release should retain execution session until idle cleanup: close=%d", client.closeCount)
	}
	second, err := svc.Run(context.Background(), scriptcontract.RunRequest{Catalog: skillcontract.CatalogRequest{}, Skill: "demo", Command: `./scripts/make_output.cmd`, Binding: testBinding("remote-run-2"), StateRoot: testStateRoot(root), ProjectDir: root, Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"}})
	if err != nil || !second.OK {
		t.Fatalf("second run result=%+v err=%v", second, err)
	}
	if client.openCount != 1 {
		t.Fatalf("same Agent conversation opened %d containers, want 1", client.openCount)
	}
	svc.ReleaseRun(context.Background(), workmodel.PreparedRun{Manifest: workmodel.RunManifest{RunID: "remote-run-2"}})
	deadline := time.Now().Add(time.Second)
	for client.closeCount != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if client.closeCount != 1 {
		t.Fatalf("idle execution container close count=%d, want 1", client.closeCount)
	}
	third, err := svc.Run(context.Background(), scriptcontract.RunRequest{Catalog: skillcontract.CatalogRequest{}, Skill: "demo", Command: `./scripts/make_output.cmd`, Binding: testBinding("remote-run-3"), StateRoot: testStateRoot(root), ProjectDir: root, Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"}})
	if err != nil || !third.OK {
		t.Fatalf("third run result=%+v err=%v", third, err)
	}
	if client.openCount != 2 || client.lastOptions.Workspace.ID != "workspace-1" {
		t.Fatalf("idle reopen did not attach durable workspace: opens=%d workspace=%+v", client.openCount, client.lastOptions.Workspace)
	}
	if err := svc.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	fourth, err := svc.Run(context.Background(), scriptcontract.RunRequest{Catalog: skillcontract.CatalogRequest{}, Skill: "demo", Command: `./scripts/make_output.cmd`, Binding: testBinding("remote-run-4"), StateRoot: testStateRoot(root), ProjectDir: root, Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"}})
	if err != nil {
		t.Fatal(err)
	}
	if fourth.OK || !strings.Contains(fourth.Error, "service 已关闭") || client.openCount != 2 {
		t.Fatalf("closed service accepted a new remote execution: result=%+v opens=%d", fourth, client.openCount)
	}
}

func TestExecutionSessionKeyReusesConversationAndSeparatesUsers(t *testing.T) {
	ctx := context.Background()
	leftBinding := testBinding("run-1")
	rightBinding := testBinding("run-2")
	profile := execmodel.SandboxProfile{Provider: "genesis-sandbox", RuntimeProfile: execmodel.RuntimeProfileOfficeBasic}
	left := sandboxsession.ExecutionKey(ctx, leftBinding, profile, sandboxcontract.WorkspaceRef{})
	right := sandboxsession.ExecutionKey(ctx, rightBinding, profile, sandboxcontract.WorkspaceRef{})
	if left != right {
		t.Fatalf("同一 Agent 会话的不同 Run 应复用 execution session: left=%q right=%q", left, right)
	}
	rightBinding.Owner.UserID = "another-user"
	if left == sandboxsession.ExecutionKey(ctx, rightBinding, profile, sandboxcontract.WorkspaceRef{}) {
		t.Fatal("不同用户不能复用有状态 execution session")
	}
	rightBinding = testBinding("run-2")
	rightBinding.Access = execmodel.WorkspaceAccessReadOnly
	if left == sandboxsession.ExecutionKey(ctx, rightBinding, profile, sandboxcontract.WorkspaceRef{}) {
		t.Fatal("不同 Workspace 访问策略不能复用 execution session")
	}
	rightBinding = testBinding("run-2")
	profile.RiskLevel = execmodel.SandboxRiskHigh
	if left == sandboxsession.ExecutionKey(ctx, rightBinding, profile, sandboxcontract.WorkspaceRef{}) {
		t.Fatal("不同风险策略不能复用 execution session")
	}
}

func TestSkillCommandServiceRestagesInputsOverExistingRemoteFiles(t *testing.T) {
	client := newFakeRemoteClient()
	source, err := embedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, skillmodel.ScopeSystem, fstest.MapFS{
		"demo/SKILL.md":                {Data: []byte("---\nname: demo\ndescription: demo skill\nallowed-tools:\n  - run_skill_command\n---\nDemo")},
		"demo/scripts/make_output.cmd": {Data: []byte("@echo off\r\necho remote>output.txt\r\n")},
	}, skillparser.New())
	if err != nil {
		t.Fatal(err)
	}
	skills := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	svc, err := New(Deps{Skills: skills, Runner: nilRunner{}, Approval: allowAllApproval{}, SessionClient: client, FileClient: client, WorkspaceRef: sandboxcontract.WorkspaceRef{ID: "w1", Provider: "genesis-sandbox"}, Provisioner: testProvisioner{}, ProducedResources: &collectingProducedRegistrar{}, RemoteSessions: noOpRemoteSessionBinder{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	root := t.TempDir()
	req := scriptcontract.RunRequest{Catalog: skillcontract.CatalogRequest{}, Skill: "demo", Command: `./scripts/make_output.cmd`, Binding: testBinding("remote-run"), StateRoot: testStateRoot(root), ProjectDir: root, Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"}}
	first, err := svc.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !first.OK || !containsProduced(first.Produced, "output.txt") {
		t.Fatalf("first result=%+v", first)
	}
	if err := os.WriteFile(filepath.Join(root, "output.txt"), []byte("fresh input"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, snapshots := testInputManifest(req.Binding, "output.txt", []byte("fresh input"))
	svc.inputSnapshots = snapshots
	req.Inputs = manifest
	second, err := svc.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !second.OK {
		t.Fatalf("second result=%+v", second)
	}
}

type collectingProducedRegistrar struct {
	registrations []workcontract.RegisterProducedResourceRequest
}

func (r *collectingProducedRegistrar) RegisterProducedResource(_ context.Context, registration workcontract.RegisterProducedResourceRequest) (workmodel.ProducedResourceDescriptor, error) {
	r.registrations = append(r.registrations, registration)
	return workmodel.ProducedResourceDescriptor{ID: fmt.Sprintf("produced-%d", len(r.registrations)), ObservedName: registration.ObservedName, MediaType: registration.MediaType}, nil
}

type noOpRemoteSessionBinder struct{}

func (noOpRemoteSessionBinder) BindRemoteSession(context.Context, string, string, string, sandboxcontract.WorkspaceRef, time.Time) error {
	return nil
}
func (noOpRemoteSessionBinder) LoadExecutionSession(context.Context, string) (sandboxcontract.WorkspaceRef, bool, error) {
	return sandboxcontract.WorkspaceRef{}, false, nil
}
func (noOpRemoteSessionBinder) SaveExecutionSession(context.Context, string, sandboxcontract.WorkspaceRef) error {
	return nil
}
func (noOpRemoteSessionBinder) DeleteExecutionSession(context.Context, string) error { return nil }

type nilRunner struct{}

func (nilRunner) Run(context.Context, execmodel.Command, execcontract.RunOptions) (*execmodel.Result, error) {
	return &execmodel.Result{ExitCode: 0}, nil
}

func (allowAllApproval) GetDecision(context.Context, string) (approvalmodel.Decision, bool) {
	return approvalmodel.Decision{}, false
}
