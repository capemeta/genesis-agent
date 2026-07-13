package service

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	execservice "genesis-agent/internal/capabilities/execution/service"
	"genesis-agent/internal/capabilities/skill/adapter/embedded"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	localexec "genesis-agent/shared/local/execution"
)

type allowAllApproval struct{}

func (allowAllApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

func TestSkillCommandServiceRunsLocalSkillCommand(t *testing.T) {
	svc := newTestService(t, fstest.MapFS{
		"demo/SKILL.md":               {Data: []byte("---\nname: demo\ndescription: demo skill\nallowed-tools:\n  - run_skill_command\n---\nDemo")},
		"demo/scripts/make_output.py": {Data: []byte("from pathlib import Path\nPath(\"made.txt\").write_text(\"made\", encoding=\"utf-8\")\n")},
	})
	root := t.TempDir()
	result, err := svc.Run(context.Background(), skillRunRequest("demo", `python scripts/make_output.py`, root))
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result=%+v", result)
	}
	if result.WorkDir == "" || result.SkillDir == "" {
		t.Fatalf("missing workdir: %+v", result)
	}
	if got := filepath.Join(result.WorkDir, "made.txt"); !containsProduced(result.Produced, "made.txt") {
		t.Fatalf("expected produced made.txt, produced=%v path=%s", result.Produced, got)
	}
	wantArtifact := filepath.Join(result.WorkDir, "made.txt")
	if len(result.Artifacts) != 1 || filepath.Clean(result.Artifacts[0].Path) != filepath.Clean(wantArtifact) {
		t.Fatalf("artifact should stay in skill work dir: %+v", result.Artifacts)
	}
	if _, err := os.Stat(filepath.Join(root, "made.txt")); !os.IsNotExist(err) {
		t.Fatalf("skill artifact leaked to workspace root, err=%v", err)
	}
}

func TestSkillCommandServiceRejectsHelperModuleEntry(t *testing.T) {
	svc := newTestService(t, fstest.MapFS{
		"demo/SKILL.md":                 {Data: []byte("---\nname: demo\ndescription: demo skill\nallowed-tools:\n  - run_skill_command\n---\nDemo")},
		"demo/scripts/path_contract.py": {Data: []byte("print('bad')")},
	})
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:       skillcontract.CatalogRequest{},
		Skill:         "demo",
		Command:       "python scripts/path_contract.py",
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || !strings.Contains(result.Error, "辅助模块") {
		t.Fatalf("result=%+v", result)
	}
}

func TestSkillCommandServiceRejectsDependencyInstallCommand(t *testing.T) {
	svc := newTestService(t, fstest.MapFS{
		"demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: demo skill\nallowed-tools:\n  - run_skill_command\ndependencies:\n  runtime:\n    node:\n      - name: pptxgenjs\n        require: pptxgenjs\n---\nDemo")},
	})
	result, err := svc.Run(context.Background(), skillRunRequest("demo", "npm install pptxgenjs", t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.FailureKind != "dependency_install_forbidden" {
		t.Fatalf("result=%+v", result)
	}
	if result.Retryable {
		t.Fatalf("install command should not be retried: %+v", result)
	}
	if len(result.Missing) != 1 || result.Missing[0].Name != "pptxgenjs" {
		t.Fatalf("missing=%+v", result.Missing)
	}
}

func TestSkillEnvIncludesRemoteNodeRuntimeSearchPath(t *testing.T) {
	env := skillEnv("/workspace", "/workspace/tmp")
	nodePath := env["NODE_PATH"]
	for _, want := range []string{"/workspace/node_modules", "/opt/genesis-sandbox/image/node_modules"} {
		if !strings.Contains(nodePath, want) {
			t.Fatalf("NODE_PATH missing %q: %s", want, nodePath)
		}
	}
}

func TestSkillEnvUsesControlledLocalDependencyPaths(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, ".genesis", "runs", "run-1", "work", "skills", "office-ppt")
	depRoot := filepath.Join(root, ".genesis", "skill-deps", "office-ppt")
	binDir := filepath.Join(depRoot, "venv", "bin")
	pyName := "python"
	if runtime.GOOS == "windows" {
		binDir = filepath.Join(depRoot, "venv", "Scripts")
		pyName = "python.exe"
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, pyName), []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	env := skillEnv(workDir, filepath.Join(root, ".genesis", "runs", "run-1", "tmp"))

	if !strings.Contains(env["NODE_PATH"], filepath.Join(depRoot, "node", "node_modules")) {
		t.Fatalf("NODE_PATH missing controlled dependency dir: %s", env["NODE_PATH"])
	}
	if env["VIRTUAL_ENV"] != filepath.Join(depRoot, "venv") {
		t.Fatalf("VIRTUAL_ENV=%q", env["VIRTUAL_ENV"])
	}
	if !strings.Contains(env["PATH"], binDir) {
		t.Fatalf("PATH missing venv bin: %s", env["PATH"])
	}
	if strings.Contains(env["NODE_PATH"], filepath.Join(root, "node_modules")) {
		t.Fatalf("NODE_PATH should not include workspace root node_modules: %s", env["NODE_PATH"])
	}
}

func skillRunRequest(skill, command, root string) scriptcontract.RunRequest {
	return scriptcontract.RunRequest{Catalog: skillcontract.CatalogRequest{}, Skill: skill, Command: command, RunID: "test-run", WorkspaceRoot: root, Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled}}
}

func newTestService(t *testing.T, fsys fstest.MapFS) *Service {
	t.Helper()
	source, err := embedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, skillmodel.ScopeSystem, fsys, skillparser.New())
	if err != nil {
		t.Fatal(err)
	}
	skills := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	runner, err := execservice.NewRunner(localexec.NewRunner(), nil)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Deps{Skills: skills, Runner: runner, Approval: allowAllApproval{}})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func containsProduced(values []string, suffix string) bool {
	for _, value := range values {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

func TestShouldIgnoreProducedPath(t *testing.T) {
	if !shouldIgnoreProducedPath("scripts/office/__pycache__/soffice.cpython-312.pyc") {
		t.Fatal("expected ignore pycache pyc")
	}
	if !shouldIgnoreProducedPath("__pycache__/x.pyc") {
		t.Fatal("expected ignore root pycache")
	}
	if shouldIgnoreProducedPath("thumbnails.jpg") {
		t.Fatal("should keep thumbnails")
	}
	if shouldIgnoreProducedPath("ultra5-comparison-summary.pptx") {
		t.Fatal("should keep pptx")
	}
}
