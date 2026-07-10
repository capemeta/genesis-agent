package service_test

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	approvalmemory "genesis-agent/internal/capabilities/approval/adapter/memory"
	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	approvalservice "genesis-agent/internal/capabilities/approval/service"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	scriptservice "genesis-agent/internal/capabilities/skill/script/service"
	"genesis-agent/internal/platform/logger"
)

type allowPolicy struct{}

func (allowPolicy) Evaluate(ctx context.Context, req approvalmodel.Request) (approvalmodel.PolicyResult, error) {
	return approvalmodel.PolicyResult{Type: approvalmodel.PolicyAllow, Reason: "test allow"}, nil
}

type denyRequester struct{}

func (denyRequester) RequestApproval(ctx context.Context, req approvalmodel.Request, result approvalmodel.PolicyResult) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionDenied, Reason: "should not ask"}, nil
}

type fakeRunner struct {
	lastCmd  execmodel.Command
	lastOpts execcontract.RunOptions
}

func (f *fakeRunner) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	f.lastCmd = cmd
	f.lastOpts = opts
	return &execmodel.Result{ExitCode: 0, Stdout: `{"ok":true}`}, nil
}

type writingFakeRunner struct{}

func (writingFakeRunner) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	path := filepath.Join(opts.Workspace.OutputDir, "fake.pptx")
	if err := os.WriteFile(path, []byte("not a pptx"), 0o644); err != nil {
		return nil, err
	}
	return &execmodel.Result{ExitCode: 0, Stdout: `{"ok":true}`}, nil
}

func TestSkillScriptServiceMaterializeAndRunLocal(t *testing.T) {
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	runner := &fakeRunner{}
	shared, err := embedded.OfficeCommonScriptsFS()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := scriptservice.New(scriptservice.Deps{
		Skills:          skillSvc,
		Runner:          runner,
		Approval:        approval,
		Logger:          logger.NewNop(),
		SharedScriptsFS: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	input := filepath.Join(root, "sample.pptx")
	writeMinimalPPTX(t, input)

	catalog := skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       catalog,
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/inspect_pptx.py",
		Args:          []string{"sample.pptx"},
		Inputs:        []string{input},
		WorkspaceRoot: root,
		// CLI 默认会带 code-polyglot-basic；SkillScript 必须按 workload 覆盖为 office-basic。
		Sandbox: execmodel.SandboxProfile{
			Mode:           execmodel.SandboxDisabled,
			RuntimeProfile: execmodel.RuntimeProfileCodePolyglotBasic,
			TaskType:       execmodel.SandboxTaskShell,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(runner.lastCmd.Command, "inspect_pptx.py") {
		t.Fatalf("command=%q", runner.lastCmd.Command)
	}
	if runner.lastOpts.Workspace.SkillDir == "" || runner.lastOpts.Workspace.InputDir == "" {
		t.Fatalf("workspace=%+v", runner.lastOpts.Workspace)
	}
	if runner.lastOpts.Sandbox.RuntimeProfile != execmodel.RuntimeProfileOfficeBasic {
		t.Fatalf("profile=%s", runner.lastOpts.Sandbox.RuntimeProfile)
	}
	if runner.lastOpts.Sandbox.Metadata["source"] != "skill" {
		t.Fatalf("metadata=%v", runner.lastOpts.Sandbox.Metadata)
	}
}

func TestSkillScriptServiceRunsNestedOfficeScript(t *testing.T) {
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	runner := &fakeRunner{}
	shared, err := embedded.OfficeCommonScriptsFS()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := scriptservice.New(scriptservice.Deps{
		Skills:          skillSvc,
		Runner:          runner,
		Approval:        approval,
		Logger:          logger.NewNop(),
		SharedScriptsFS: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	input := filepath.Join(root, "sample.pptx")
	writeMinimalPPTX(t, input)
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI},
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/office/unpack.py",
		Args:          []string{"sample.pptx", "unpacked"},
		Inputs:        []string{input},
		WorkspaceRoot: root,
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(runner.lastCmd.Command, "office/unpack.py") && !strings.Contains(runner.lastCmd.Command, `office\unpack.py`) {
		t.Fatalf("expected nested relative script path, command=%q", runner.lastCmd.Command)
	}
	if n := result.Metadata["materialized"]; n == "" || n == "0" {
		t.Fatalf("expected shared files materialized, metadata=%v", result.Metadata)
	}
}

func TestSkillScriptServiceRejectsHelperModuleEntry(t *testing.T) {
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	svc, err := scriptservice.New(scriptservice.Deps{
		Skills:   skillSvc,
		Runner:   &fakeRunner{},
		Approval: approval,
		Logger:   logger.NewNop(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog: skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI},
		Skill:   "office-ppt",
		Script:  "office-ppt/scripts/path_contract.py",
		Args:    []string{"create", "x.pptx"},
		Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Fatalf("expected reject helper entry, got %+v", result)
	}
	if !strings.Contains(result.Error, "辅助模块") {
		t.Fatalf("error=%q", result.Error)
	}
}

func TestSkillScriptServiceFailsArtifactGate(t *testing.T) {
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	root := t.TempDir()
	svc, err := scriptservice.New(scriptservice.Deps{
		Skills:   skillSvc,
		Runner:   writingFakeRunner{},
		Approval: approval,
		Logger:   logger.NewNop(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI},
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/inspect_pptx.py",
		Args:          []string{"x.pptx"},
		WorkspaceRoot: root,
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Fatalf("expected gate failure, got %+v", result)
	}
	if !strings.Contains(result.Error, "artifact gate") {
		t.Fatalf("result=%+v", result)
	}
}

func newAllowApproval(t *testing.T) approvalcontract.Service {
	t.Helper()
	svc, err := approvalservice.New(allowPolicy{}, denyRequester{}, approvalmemory.NewStore(), logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func newEmbeddedSkillService(t *testing.T) skillcontract.Service {
	t.Helper()
	systemFS, err := embedded.SystemFS()
	if err != nil {
		t.Fatal(err)
	}
	source, err := embedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, skillmodel.ScopeSystem, systemFS, skillparser.New())
	if err != nil {
		t.Fatal(err)
	}
	return skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
}

func writeMinimalPPTX(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("[Content_Types].xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(`<?xml version="1.0"?><Types></Types>`))
	w, err = zw.Create("ppt/slides/slide1.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(`<sld/>`))
	_ = zw.Close()
	_ = f.Close()
}
