package service_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	execservice "genesis-agent/internal/capabilities/execution/service"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	scriptservice "genesis-agent/internal/capabilities/skill/script/service"
	"genesis-agent/internal/platform/logger"
	localexec "genesis-agent/shared/local/execution"
)

func TestOfficePPTInspectSmoke(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not available")
	}
	svc := newRealSkillScriptService(t)
	root := t.TempDir()
	input := filepath.Join(root, "sample.pptx")
	writeMinimalPPTX(t, input)

	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI},
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/inspect_pptx.py",
		Args:          []string{"sample.pptx"},
		Inputs:        []string{input},
		WorkspaceRoot: root,
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("inspect failed: %+v", result)
	}
	if !strings.Contains(result.Stdout, `"ok"`) {
		t.Fatalf("stdout=%s", result.Stdout)
	}
}

func TestOfficePPTThumbnailSmokeOptional(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not available")
	}
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skip("soffice not available")
	}
	if err := exec.Command("python", "-c", "import PIL").Run(); err != nil {
		t.Skip("Pillow not available")
	}
	svc := newRealSkillScriptService(t)
	root := t.TempDir()
	input := filepath.Join(root, "sample.pptx")
	writeMinimalPPTX(t, input)

	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI},
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/thumbnail.py",
		Args:          []string{"sample.pptx"},
		Inputs:        []string{input},
		WorkspaceRoot: root,
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
		TimeoutMS:     180000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("thumbnail failed: %+v stderr=%s", result, result.Stderr)
	}
}

func TestOfficePPTCreatePptxSmokeOptional(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	if err := exec.Command("node", "-e", "require('pptxgenjs')").Run(); err != nil {
		t.Skip("pptxgenjs not installed")
	}
	svc := newRealSkillScriptService(t)
	root := t.TempDir()
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI},
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/create_pptx.js",
		Args:          []string{"demo.pptx", "Smoke Title", "Smoke Subtitle"},
		WorkspaceRoot: root,
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
		TimeoutMS:     120000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("create_pptx failed: %+v stderr=%s", result, result.Stderr)
	}
	found := false
	for _, art := range result.Artifacts {
		if strings.HasSuffix(strings.ToLower(art.Name), ".pptx") && art.OK {
			found = true
		}
	}
	if !found {
		entries, _ := os.ReadDir(result.OutputDir)
		for _, e := range entries {
			if strings.HasSuffix(strings.ToLower(e.Name()), ".pptx") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected pptx artifact, result=%+v", result)
	}
}

func newRealSkillScriptService(t *testing.T) scriptcontract.Runner {
	t.Helper()
	skillSvc := newEmbeddedSkillService(t)
	approval := newAllowApproval(t)
	shared, err := embedded.OfficeCommonScriptsFS()
	if err != nil {
		t.Fatal(err)
	}
	direct := localexec.NewRunner()
	runner, err := execservice.NewRunner(direct, nil, execservice.WithLogger(logger.NewNop()))
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
	return svc
}
