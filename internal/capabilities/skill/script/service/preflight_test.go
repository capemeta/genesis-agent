package service_test

import (
	"context"
	"strings"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/capabilities/skill/adapter/embedded"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	scriptservice "genesis-agent/internal/capabilities/skill/script/service"
	"genesis-agent/internal/platform/logger"
)

type fakeInstaller struct {
	called bool
	err    error
}

func (f *fakeInstaller) InstallRuntime(ctx context.Context, skill string, missing []scriptcontract.MissingDep) error {
	f.called = true
	return f.err
}

func TestPreflightReportsMissingNodeModule(t *testing.T) {
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
		EnablePreflight: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       catalogCLI(),
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/create_pptx.js",
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Skip("pptxgenjs already installed locally; preflight passed")
	}
	if result.FailureKind != "dependency_missing" {
		t.Fatalf("kind=%q error=%q", result.FailureKind, result.Error)
	}
	if result.Metadata["backend"] != "preflight" {
		t.Fatalf("metadata=%v", result.Metadata)
	}
}

func TestAutoRetryAfterInstallOptIn(t *testing.T) {
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	shared, err := embedded.OfficeCommonScriptsFS()
	if err != nil {
		t.Fatal(err)
	}
	inst := &fakeInstaller{}
	svc, err := scriptservice.New(scriptservice.Deps{
		Skills:                skillSvc,
		Runner:                &fakeRunner{},
		Approval:              approval,
		Logger:                logger.NewNop(),
		SharedScriptsFS:       shared,
		EnablePreflight:       true,
		AutoRetryAfterInstall: true,
		Installer:             inst,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       catalogCLI(),
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/create_pptx.js",
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK && !inst.called {
		t.Skip("pptxgenjs present; no install path")
	}
	if inst.called && result.Metadata["auto_retried"] != "true" {
		t.Fatalf("expected auto_retried, metadata=%v warnings=%v", result.Metadata, result.Warnings)
	}
	if inst.called && !strings.Contains(strings.Join(result.Warnings, "\n"), "auto_retry_after_install") {
		t.Fatalf("warnings=%v", result.Warnings)
	}
}
