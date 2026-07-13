package service

import (
	"context"
	"os"
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
	"genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	skillservice "genesis-agent/internal/capabilities/skill/service"
)

type fakeRemoteClient struct {
	files       map[string][]byte
	lastCommand execmodel.Command
	runCount    int
}

func newFakeRemoteClient() *fakeRemoteClient { return &fakeRemoteClient{files: map[string][]byte{}} }
func (c *fakeRemoteClient) OpenSession(context.Context, sandboxcontract.SessionOptions) (sandboxcontract.SandboxSession, error) {
	return &fakeRemoteSession{client: c}, nil
}
func (c *fakeRemoteClient) ReadFile(_ context.Context, req sandboxcontract.FileRequest, _ fscontract.ReadOptions) ([]byte, error) {
	return append([]byte(nil), c.files[normalizeSlash(req.Path.WorkspaceRel)]...), nil
}
func (c *fakeRemoteClient) WriteFile(_ context.Context, req sandboxcontract.WriteFileRequest) error {
	c.files[normalizeSlash(req.Path.WorkspaceRel)] = append([]byte(nil), req.Content...)
	return nil
}
func (c *fakeRemoteClient) ListDir(context.Context, sandboxcontract.ListDirRequest) ([]fsmodel.DirEntry, error) {
	return nil, nil
}
func (c *fakeRemoteClient) Walk(_ context.Context, req sandboxcontract.WalkRequest) (*fsmodel.WalkOutcome, error) {
	entries := make([]fsmodel.DirEntry, 0, len(c.files))
	modifiedAt := time.Unix(1, 0)
	for path, data := range c.files {
		entries = append(entries, fsmodel.DirEntry{Name: path, Path: path, Type: fsmodel.EntryTypeFile, Size: int64(len(data)), ModifiedAt: modifiedAt})
	}
	return &fsmodel.WalkOutcome{Root: req.Path.WorkspaceRel, Entries: entries}, nil
}
func (c *fakeRemoteClient) Stat(_ context.Context, req sandboxcontract.FileRequest) (*fsmodel.FileStat, error) {
	path := normalizeSlash(req.Path.WorkspaceRel)
	data, ok := c.files[path]
	if !ok {
		return nil, nil
	}
	return &fsmodel.FileStat{Path: fsmodel.ResolvedPath{WorkspaceRel: path}, Type: fsmodel.EntryTypeFile, Size: int64(len(data)), ModifiedAt: time.Unix(1, 0)}, nil
}
func (c *fakeRemoteClient) MkdirAll(context.Context, sandboxcontract.MkdirRequest) error { return nil }
func (c *fakeRemoteClient) Remove(context.Context, sandboxcontract.RemoveRequest) error  { return nil }

type fakeRemoteSession struct{ client *fakeRemoteClient }

func (s *fakeRemoteSession) Workspace() sandboxcontract.WorkspaceRef {
	return sandboxcontract.WorkspaceRef{ID: "session-1", Provider: "genesis-sandbox"}
}
func (s *fakeRemoteSession) Run(_ context.Context, req sandboxcontract.CommandRequest) (*execmodel.Result, error) {
	s.client.lastCommand = req.Command
	s.client.runCount++
	s.client.files["output.txt"] = []byte("done")
	return &execmodel.Result{ExitCode: 0, Stdout: "ok"}, nil
}
func (s *fakeRemoteSession) Close(context.Context) error { return nil }

func TestSkillCommandServiceRunsInRemoteSessionWorkspace(t *testing.T) {
	client := newFakeRemoteClient()
	source, err := embedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, skillmodel.ScopeSystem, fstest.MapFS{
		"demo/SKILL.md":                {Data: []byte("---\nname: demo\ndescription: demo skill\nallowed-tools:\n  - run_skill_command\ndependencies:\n  runtime:\n    system:\n      - name: libreoffice\n        command: soffice\n---\nDemo")},
		"demo/scripts/make_output.cmd": {Data: []byte("@echo off\r\necho remote>output.txt\r\n")},
	}, skillparser.New())
	if err != nil {
		t.Fatal(err)
	}
	skills := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	svc, err := New(Deps{Skills: skills, Runner: nilRunner{}, Approval: allowAllApproval{}, SessionClient: client, FileClient: client, WorkspaceRef: sandboxcontract.WorkspaceRef{ID: "w1", Provider: "genesis-sandbox"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{Catalog: skillcontract.CatalogRequest{}, Skill: "demo", Command: `./scripts/make_output.cmd`, RunID: "remote-run", WorkspaceRoot: t.TempDir(), Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result=%+v", result)
	}
	if client.lastCommand.Cwd != "/workspace" {
		t.Fatalf("cwd=%q", client.lastCommand.Cwd)
	}
	if !strings.Contains(client.lastCommand.Command, "scripts") {
		t.Fatalf("command=%q", client.lastCommand.Command)
	}
	if _, ok := client.files["scripts/make_output.cmd"]; !ok {
		t.Fatalf("materialized files=%v", client.files)
	}
	if !containsProduced(result.Produced, "output.txt") {
		t.Fatalf("produced=%v", result.Produced)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].Name != "output.txt" || !result.Artifacts[0].OK {
		t.Fatalf("artifacts=%+v warnings=%v", result.Artifacts, result.Warnings)
	}
	if _, err := os.Stat(result.Artifacts[0].Path); err != nil {
		t.Fatalf("artifact not materialized locally: %v path=%q", err, result.Artifacts[0].Path)
	}
	if !strings.Contains(filepath.ToSlash(result.Artifacts[0].Path), "/artifacts/demo/output.txt") {
		t.Fatalf("artifact path=%q", result.Artifacts[0].Path)
	}
}

type nilRunner struct{}

func (nilRunner) Run(context.Context, execmodel.Command, execcontract.RunOptions) (*execmodel.Result, error) {
	return &execmodel.Result{ExitCode: 0}, nil
}

func (allowAllApproval) GetDecision(context.Context, string) (approvalmodel.Decision, bool) {
	return approvalmodel.Decision{}, false
}
