package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	sandboxhttp "genesis-agent/internal/capabilities/sandbox/adapter/http"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	"genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	scriptworkspace "genesis-agent/internal/capabilities/skill/script/workspace"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	"genesis-agent/internal/platform/contextutil"
)

func TestRemoteSkillCommandStagesWorkDirInputAndUsesImageNodeModules(t *testing.T) {
	if os.Getenv("GENESIS_AGENT_REMOTE_SKILL_SMOKE") != "1" {
		t.Skip("set GENESIS_AGENT_REMOTE_SKILL_SMOKE=1 to run against a live genesis-sandbox")
	}
	apiKey := strings.TrimSpace(os.Getenv("GENESIS_SANDBOX_API_KEY"))
	if apiKey == "" {
		t.Skip("GENESIS_SANDBOX_API_KEY is required")
	}
	baseURL := strings.TrimSpace(os.Getenv("GENESIS_SANDBOX_BASE_URL"))
	if baseURL == "" {
		baseURL = "http://127.0.0.1:18010"
	}
	client, err := sandboxhttp.New(sandboxhttp.Config{BaseURL: baseURL, APIKey: apiKey, Timeout: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	fsys, err := embedded.SystemFS()
	if err != nil {
		t.Fatal(err)
	}
	source, err := embedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "system"}, skillmodel.ScopeSystem, fsys, skillparser.New())
	if err != nil {
		t.Fatal(err)
	}
	skills := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	svc, err := New(Deps{
		Skills:        skills,
		Runner:        noopExecutionRunner{},
		Approval:      allowAllApproval{},
		SessionClient: client,
		FileClient:    client,
		WorkspaceRef:  sandboxcontract.WorkspaceRef{Provider: "genesis-sandbox"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = svc.Close(context.Background())
	})

	root := t.TempDir()
	runID := "remote-smoke"
	ws, err := scriptworkspace.PrepareLocalTask(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	const scriptName = "deck_gen.js"
	script := `
(async () => {
  const pptxgen = require("pptxgenjs");
  const pres = new pptxgen();
  pres.layout = "LAYOUT_16x9";
  const slide = pres.addSlide();
  slide.addText("remote smoke", { x: 1, y: 1, w: 8, h: 1, fontSize: 28 });
  await pres.writeFile({ fileName: "smoke.pptx" });
})().catch((err) => {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
`
	if err := os.WriteFile(filepath.Join(ws.WorkDir, scriptName), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := contextutil.WithRunID(context.Background(), runID)
	result, err := svc.Run(ctx, scriptcontract.RunRequest{
		Skill:         "office-ppt",
		Command:       "node " + scriptName,
		Inputs:        []string{"$WORK_DIR/" + scriptName},
		RunID:         runID,
		TimeoutMS:     120000,
		WorkspaceRoot: root,
		Sandbox: execmodel.SandboxProfile{
			Mode:     execmodel.SandboxRequired,
			Provider: "genesis-sandbox",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result=%+v", result)
	}
	if !containsProduced(result.Produced, "smoke.pptx") {
		t.Fatalf("produced=%v", result.Produced)
	}
	wantRel := ".genesis/runs/remote-smoke/output/office-ppt/smoke.pptx"
	if len(result.Artifacts) != 1 || result.Artifacts[0].Path != wantRel {
		t.Fatalf("artifact path for model should be workspace-relative: %+v want=%q", result.Artifacts, wantRel)
	}
	if filepath.IsAbs(filepath.FromSlash(result.Artifacts[0].Path)) {
		t.Fatalf("artifact must not expose host absolute path: %q", result.Artifacts[0].Path)
	}
	if _, err := os.Stat(filepath.Join(root, ".genesis", "runs", "remote-smoke-materialize")); !os.IsNotExist(err) {
		t.Fatalf("should not create separate -materialize run dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "smoke.pptx")); !os.IsNotExist(err) {
		t.Fatalf("remote artifact leaked to workspace root, err=%v", err)
	}
}

type noopExecutionRunner struct{}

func (noopExecutionRunner) Run(context.Context, execmodel.Command, execcontract.RunOptions) (*execmodel.Result, error) {
	return nil, fmt.Errorf("noopExecutionRunner should not be used by remote smoke")
}
