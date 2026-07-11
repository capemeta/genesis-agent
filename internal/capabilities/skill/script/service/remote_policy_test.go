package service_test

import (
	"context"
	"io"
	"strings"
	"testing"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	"genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	scriptservice "genesis-agent/internal/capabilities/skill/script/service"
	"genesis-agent/internal/platform/logger"
)

type recordingSession struct {
	staged  []string
	lastRun sandboxcontract.CommandRequest
}

func (s *recordingSession) StageInput(ctx context.Context, req sandboxcontract.StageInputRequest) (*sandboxcontract.StageInputResult, error) {
	s.staged = append(s.staged, req.Name)
	if req.Content != nil {
		_, _ = io.Copy(io.Discard, req.Content)
	}
	return &sandboxcontract.StageInputResult{
		Artifact: execmodel.InputArtifactRef{Name: req.Name},
	}, nil
}

func (s *recordingSession) Run(ctx context.Context, req sandboxcontract.CommandRequest) (*execmodel.Result, error) {
	s.lastRun = req
	return &execmodel.Result{ExitCode: 0, Stdout: `{"ok":true}`}, nil
}

func (s *recordingSession) Close(context.Context) error { return nil }

type recordingSessionClient struct {
	session *recordingSession
}

func (c *recordingSessionClient) OpenSession(ctx context.Context, opts sandboxcontract.SessionOptions) (sandboxcontract.SandboxSession, error) {
	if c.session == nil {
		c.session = &recordingSession{}
	}
	return c.session, nil
}

type unavailableSessionClient struct{}

func (unavailableSessionClient) OpenSession(ctx context.Context, opts sandboxcontract.SessionOptions) (sandboxcontract.SandboxSession, error) {
	return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, context.DeadlineExceeded)
}

func catalogCLI() skillcontract.CatalogRequest {
	return skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI, Environment: profilemodel.EnvironmentLocal}
}

func TestRemoteStageInputUsesSkillScriptsZip(t *testing.T) {
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	client := &recordingSessionClient{}
	shared, err := embedded.OfficeCommonScriptsFS()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := scriptservice.New(scriptservice.Deps{
		Skills:          skillSvc,
		Runner:          &fakeRunner{},
		Approval:        approval,
		SessionClient:   client,
		Logger:          logger.NewNop(),
		SharedScriptsFS: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog: catalogCLI(),
		Skill:   "office-ppt",
		Script:  "office-ppt/scripts/office/unpack.py",
		Sandbox: execmodel.SandboxProfile{
			Mode:     execmodel.SandboxRequired,
			Provider: "genesis-sandbox",
		},
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.OK {
		t.Fatalf("result=%+v", result)
	}
	if client.session == nil {
		t.Fatal("session not opened")
	}
	found := false
	for _, name := range client.session.staged {
		if name == "skill-scripts-office-ppt.zip" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("skill scripts zip missing; staged=%v", client.session.staged)
	}
	if result.Metadata["backend"] != "remote_session" {
		t.Fatalf("backend=%v", result.Metadata)
	}
}

func TestRemoteSkillScriptUsesAbsoluteSandboxEntryAndScriptsCwd(t *testing.T) {
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	client := &recordingSessionClient{}
	shared, err := embedded.OfficeCommonScriptsFS()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := scriptservice.New(scriptservice.Deps{
		Skills:          skillSvc,
		Runner:          &fakeRunner{},
		Approval:        approval,
		SessionClient:   client,
		Logger:          logger.NewNop(),
		SharedScriptsFS: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       catalogCLI(),
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/run_pptxgen_script.js",
		Args:          []string{"deck_gen.js"},
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"},
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.OK {
		t.Fatalf("result=%+v", result)
	}
	cmd := client.session.lastRun.Command
	if !strings.Contains(cmd.Command, "/workspace/input/skill-scripts-office-ppt.zip") {
		t.Fatalf("command should unzip staged scripts first, got %q", cmd.Command)
	}
	if !strings.Contains(cmd.Command, "/workspace/tmp/skills/office-ppt/scripts/run_pptxgen_script.js") {
		t.Fatalf("command should use absolute sandbox script path, got %q", cmd.Command)
	}
	if cmd.Cwd != "/workspace" {
		t.Fatalf("cwd=%q", cmd.Cwd)
	}
	if !strings.Contains(cmd.Command, "cd /workspace/tmp/skills/office-ppt/scripts") {
		t.Fatalf("command should cd into scripts dir after unzip, got %q", cmd.Command)
	}
	if client.session.lastRun.Options.Workspace.SkillDir != "/workspace/tmp/skills/office-ppt" {
		t.Fatalf("workspace=%+v", client.session.lastRun.Options.Workspace)
	}
}

func TestRemoteSkillScriptExposesImageNodeModules(t *testing.T) {
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	client := &recordingSessionClient{}
	shared, err := embedded.OfficeCommonScriptsFS()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := scriptservice.New(scriptservice.Deps{
		Skills:          skillSvc,
		Runner:          &fakeRunner{},
		Approval:        approval,
		SessionClient:   client,
		Logger:          logger.NewNop(),
		SharedScriptsFS: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       catalogCLI(),
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/run_pptxgen_script.js",
		Args:          []string{"deck_gen.js"},
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"},
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.OK {
		t.Fatalf("result=%+v", result)
	}
	env := client.session.lastRun.Command.Env
	if !strings.Contains(env["NODE_PATH"], "/opt/genesis-sandbox/image/node_modules") {
		t.Fatalf("NODE_PATH=%q", env["NODE_PATH"])
	}
	if env["PYTHONPATH"] != "/workspace/tmp/skills/office-ppt/scripts" {
		t.Fatalf("PYTHONPATH=%q", env["PYTHONPATH"])
	}
}

func TestOptionalRemoteDegradesWhenSessionClientMissing(t *testing.T) {
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	shared, err := embedded.OfficeCommonScriptsFS()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := scriptservice.New(scriptservice.Deps{
		Skills:          skillSvc,
		Runner:          &fakeRunner{},
		Approval:        approval,
		Logger:          logger.NewNop(),
		SharedScriptsFS: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog: catalogCLI(),
		Skill:   "office-ppt",
		Script:  "office-ppt/scripts/inspect_pptx.py",
		Sandbox: execmodel.SandboxProfile{
			Mode:     execmodel.SandboxOptional,
			Provider: "genesis-sandbox",
		},
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata["sandbox_degraded"] != "true" || result.Metadata["backend"] != "local_degraded" {
		t.Fatalf("metadata=%v", result.Metadata)
	}
	joined := strings.Join(result.Warnings, "\n")
	if !strings.Contains(joined, "skill_script_sandbox_fallback") {
		t.Fatalf("warnings=%v", result.Warnings)
	}
}

func TestRequiredRemoteFailsClosedWithoutSessionClient(t *testing.T) {
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	shared, err := embedded.OfficeCommonScriptsFS()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := scriptservice.New(scriptservice.Deps{
		Skills:          skillSvc,
		Runner:          &fakeRunner{},
		Approval:        approval,
		Logger:          logger.NewNop(),
		SharedScriptsFS: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog: catalogCLI(),
		Skill:   "office-ppt",
		Script:  "office-ppt/scripts/inspect_pptx.py",
		Sandbox: execmodel.SandboxProfile{
			Mode:     execmodel.SandboxRequired,
			Provider: "genesis-sandbox",
		},
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Fatal("expected failure")
	}
	if !strings.Contains(result.Error, "required") && !strings.Contains(result.Error, "SessionClient") {
		t.Fatalf("error=%q", result.Error)
	}
	if result.Metadata["sandbox_degraded"] == "true" {
		t.Fatal("required must not degrade")
	}
}

func TestOptionalRemoteDegradesOnSandboxUnavailable(t *testing.T) {
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	shared, err := embedded.OfficeCommonScriptsFS()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := scriptservice.New(scriptservice.Deps{
		Skills:          skillSvc,
		Runner:          &fakeRunner{},
		Approval:        approval,
		SessionClient:   unavailableSessionClient{},
		Logger:          logger.NewNop(),
		SharedScriptsFS: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog: catalogCLI(),
		Skill:   "office-ppt",
		Script:  "office-ppt/scripts/inspect_pptx.py",
		Sandbox: execmodel.SandboxProfile{
			Mode:     execmodel.SandboxOptional,
			Provider: "genesis-sandbox",
		},
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata["sandbox_degraded"] != "true" {
		t.Fatalf("metadata=%v", result.Metadata)
	}
}
