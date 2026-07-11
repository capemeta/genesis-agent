package service_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	execservice "genesis-agent/internal/capabilities/execution/service"
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

func TestOfficePPTExtractTextSmoke(t *testing.T) {
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
		Script:        "office-ppt/scripts/extract_pptx_text.py",
		Args:          []string{"sample.pptx", "--format", "markdown", "--include-empty"},
		Inputs:        []string{input},
		WorkspaceRoot: root,
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("extract text failed: %+v", result)
	}
	if !strings.Contains(result.Stdout, "# sample.pptx") || !strings.Contains(result.Stdout, "## Slide 1") {
		t.Fatalf("stdout=%s", result.Stdout)
	}
}

func TestOfficePPTCreatePptxFromSpecSmokeOptional(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	if err := exec.Command("node", "-e", "require('pptxgenjs')").Run(); err != nil {
		t.Skip("pptxgenjs not installed")
	}
	svc := newRealSkillScriptService(t)
	root := t.TempDir()
	spec := `{
  "title": "Intel Ultra 5-225H 对比",
  "subtitle": "2026主流轻薄本推荐",
  "slides": [
    {"type":"title","title":"Intel Ultra 5-225H 对比","subtitle":"32GB + 1TB 轻薄创作本"},
    {"type":"bullets","title":"核心优势","bullets":["性能跃升","核显进化","屏幕革命"]},
    {"type":"table","title":"机型概览","table":{"headers":["型号","价格"],"rows":[["Yoga Slim 7","7299"],["Zenbook S 14","7999"]]}},
    {"type":"two_column","title":"选购建议","columns":[{"heading":"创作","bullets":["Zenbook S 14 OLED"]},{"heading":"移动办公","bullets":["Yoga Slim 7 Gen 9"]}]}
  ]
}`
	if err := os.WriteFile(filepath.Join(root, "deck.json"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI},
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/create_pptx.js",
		Args:          []string{"deck.pptx", "--spec", "deck.json"},
		Inputs:        []string{"deck.json"},
		WorkspaceRoot: root,
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
		TimeoutMS:     120000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || !strings.Contains(result.Stdout, `"slides": 4`) {
		t.Fatalf("create from spec failed: result=%+v stdout=%s stderr=%s", result, result.Stdout, result.Stderr)
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

func TestOfficePPTRunPptxgenScriptSmokeOptional(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	if err := exec.Command("node", "-e", "require('pptxgenjs')").Run(); err != nil {
		t.Skip("pptxgenjs not installed")
	}
	svc := newRealSkillScriptService(t)
	root := t.TempDir()
	script := `const pptxgen = require("pptxgenjs");
const path = require("path");
const fs = require("fs");
const pres = new pptxgen();
pres.layout = "LAYOUT_16x9";
pres.title = "Runner Smoke";
const s1 = pres.addSlide();
s1.addText("封面", { x: 0.5, y: 2, w: 9, h: 1, fontSize: 36, bold: true });
const s2 = pres.addSlide();
s2.addText("第二页", { x: 0.5, y: 0.5, w: 9, h: 0.6, fontSize: 28, bold: true });
s2.addText([
  { text: "中文要点一", options: { bullet: true, breakLine: true } },
  { text: "中文要点二", options: { bullet: true } }
], { x: 0.5, y: 1.3, w: 9, h: 2, fontSize: 16 });
const out = path.join(process.env.OUTPUT_DIR || ".", "runner-smoke.pptx");
pres.writeFile({ fileName: out }).then(() => {
  const st = fs.statSync(out);
  console.log(JSON.stringify({ ok: true, output: "runner-smoke.pptx", size_bytes: st.size, slides: 2 }));
});
`
	if err := os.WriteFile(filepath.Join(root, "deck_gen.js"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI},
		Skill:         "office-ppt",
		Script:        "office-ppt/scripts/run_pptxgen_script.js",
		Args:          []string{"deck_gen.js"},
		Inputs:        []string{"deck_gen.js"},
		WorkspaceRoot: root,
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
		TimeoutMS:     120000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("run_pptxgen_script failed: %+v stdout=%s stderr=%s", result, result.Stdout, result.Stderr)
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
		t.Fatalf("expected pptx artifact, result=%+v stdout=%s", result, result.Stdout)
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
