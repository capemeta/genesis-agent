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
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	localexec "genesis-agent/shared/local/execution"
)

type allowAllApproval struct{ calls *int }

func (a allowAllApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	if a.calls != nil {
		*a.calls = *a.calls + 1
	}
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

func TestSkillCommandMissingGeneratedEntryIsBlockedBeforeApproval(t *testing.T) {
	approvalCalls := 0
	svc := newTestServiceWithApproval(t, fstest.MapFS{
		"demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: demo skill\nallowed-tools:\n  - run_skill_command\n---\nDemo")},
	}, allowAllApproval{calls: &approvalCalls})
	result, err := svc.Run(context.Background(), skillRunRequest("demo", "node create_ppt.js", t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.FailureKind != "input_binding_missing" || result.SuggestedAction != "stage_command_entry_via_inputs" || !result.Retryable {
		t.Fatalf("result=%+v", result)
	}
	if approvalCalls != 0 || result.ApprovalDurationMS != 0 || result.ExecutionDurationMS != 0 {
		t.Fatalf("缺失入口必须在审批和执行前阻断: calls=%d result=%+v", approvalCalls, result)
	}
	if len(result.RequiredInputs) != 1 || result.RequiredInputs[0] != "$WORK_DIR/create_ppt.js" || result.ExactCall == nil {
		t.Fatalf("缺少精确 inputs 纠错: %+v", result)
	}
	args := result.ExactCall.Arguments
	inputs, _ := args["inputs"].([]string)
	if result.ExactCall.Tool != "run_skill_command" || args["command"] != "node create_ppt.js" || len(inputs) != 1 || inputs[0] != "$WORK_DIR/create_ppt.js" {
		t.Fatalf("exact_call=%+v", result.ExactCall)
	}
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
	if result.DurationMS < result.ExecutionDurationMS || result.ExecutionDurationMS <= 0 || result.ApprovalDurationMS < 0 || result.StagingDurationMS < 0 {
		t.Fatalf("分阶段耗时无效: total=%d approval=%d staging=%d execution=%d", result.DurationMS, result.ApprovalDurationMS, result.StagingDurationMS, result.ExecutionDurationMS)
	}
	if result.WorkDir == "" || result.SkillDir == "" {
		t.Fatalf("missing workdir: %+v", result)
	}
	wantWorkRel := filepath.ToSlash(filepath.Join(".genesis", "runtime", "runs", "test-run", "work", "test-run-skill-demo", "skills", "demo"))
	if result.WorkDir != wantWorkRel || result.SkillDir != wantWorkRel {
		t.Fatalf("host-backed work/skill dir should be workspace-relative: work=%q skill=%q want=%q", result.WorkDir, result.SkillDir, wantWorkRel)
	}
	if result.Metadata[metaExecutionBackend] != executionBackendLocalHost {
		t.Fatalf("execution_backend=%q", result.Metadata[metaExecutionBackend])
	}
	if _, ok := result.Metadata["path_map"]; ok {
		t.Fatalf("path_map must not be returned to model: %v", result.Metadata)
	}
	if got := filepath.Join(root, filepath.FromSlash(result.WorkDir), "made.txt"); !containsProduced(result.Produced, "made.txt") {
		t.Fatalf("expected produced made.txt, produced=%v path=%s", result.Produced, got)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(result.WorkDir), "made.txt")); err != nil {
		t.Fatalf("produced file should still exist in skill work dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "made.txt")); !os.IsNotExist(err) {
		t.Fatalf("skill artifact leaked to workspace root, err=%v", err)
	}
}

func TestValidateInputManifestRejectsPackageFileCollision(t *testing.T) {
	binding := testBinding("collision-run")
	pkg := skillmodel.SkillPackageSnapshot{PackageID: "demo", Files: []skillmodel.PackageFileDigest{{Resource: "demo/scripts/run.py"}}}
	manifest := workmodel.InputManifest{
		RunID: binding.Owner.RunID, BindingID: binding.ID,
		Inputs: []workmodel.InputRef{{ID: "input-1", Alias: "scripts/run.py"}},
	}
	if err := validateInputManifest(binding, pkg, manifest); err == nil || !strings.Contains(err.Error(), "不可变Skill包文件冲突") {
		t.Fatalf("expected package collision rejection, got %v", err)
	}
}

func TestSkillCommandServiceRejectsHelperModuleEntry(t *testing.T) {
	svc := newTestService(t, fstest.MapFS{
		"demo/SKILL.md":                 {Data: []byte("---\nname: demo\ndescription: demo skill\nallowed-tools:\n  - run_skill_command\n---\nDemo")},
		"demo/scripts/path_contract.py": {Data: []byte("print('bad')")},
	})
	result, err := svc.Run(context.Background(), scriptcontract.RunRequest{
		Catalog:    skillcontract.CatalogRequest{},
		Skill:      "demo",
		Command:    "python scripts/path_contract.py",
		Binding:    testBinding("test-run"),
		StateRoot:  testStateRoot(t.TempDir()),
		ProjectDir: t.TempDir(),
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

func TestSkillCommandServiceRejectsDeclaredRuntimeProbeBeforeExecution(t *testing.T) {
	svc := newTestService(t, fstest.MapFS{
		"demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: demo skill\nallowed-tools:\n  - run_skill_command\ndependencies:\n  runtime:\n    node:\n      - name: pptxgenjs\n        require: pptxgenjs\n---\nDemo")},
	})
	result, err := svc.Run(context.Background(), skillRunRequest("demo", `node -e "require('pptxgenjs'); console.log('ok')"`, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.FailureKind != "runtime_probe_unnecessary" || result.SuggestedAction != "run_declared_business_command_directly" {
		t.Fatalf("result=%+v", result)
	}
	if result.ApprovalDurationMS != 0 || result.ExecutionDurationMS != 0 {
		t.Fatalf("冗余探测必须在审批和执行前阻断: %+v", result)
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
	workDir := filepath.Join(root, ".genesis", "runtime", "runs", "run-1", "work", "skills", "office-ppt")
	depRoot := filepath.Join(root, ".genesis", "cache", "skill-deps", "office-ppt")
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
	env := skillEnv(workDir, filepath.Join(root, ".genesis", "runtime", "runs", "run-1", "tmp"))

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
	return scriptcontract.RunRequest{Catalog: skillcontract.CatalogRequest{}, Skill: skill, Command: command, Binding: testBinding("test-run"), StateRoot: testStateRoot(root), ProjectDir: root, Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled}}
}

func newTestService(t *testing.T, fsys fstest.MapFS) *Service {
	return newTestServiceWithApproval(t, fsys, allowAllApproval{})
}

func newTestServiceWithApproval(t *testing.T, fsys fstest.MapFS, approval allowAllApproval) *Service {
	t.Helper()
	fsys = testRuntimeSkillFS(fsys)
	source, err := embedded.NewSource(skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, skillmodel.ScopeSystem, fsys, skillparser.New())
	if err != nil {
		t.Fatal(err)
	}
	skills := testSkillService{Service: skillservice.New([]skillcontract.Source{source}, skillservice.Options{KnownTools: []string{"run_skill_command"}})}
	runner, err := execservice.NewRunner(localexec.NewRunner(), nil)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Deps{Skills: skills, Runner: runner, Approval: approval, Provisioner: testProvisioner{}, ProducedResources: &collectingProducedRegistrar{}})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func containsProduced(values []scriptcontract.ProducedCandidate, suffix string) bool {
	for _, value := range values {
		if strings.HasSuffix(value.Name, suffix) {
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
	if !shouldIgnoreProducedPath("nul") || !shouldIgnoreProducedPath("office-ppt/nul") || !shouldIgnoreProducedPath("NUL.txt") {
		t.Fatal("expected ignore reserved DOS device nul")
	}
}

func TestIsReservedDOSDeviceName(t *testing.T) {
	for _, name := range []string{"nul", "NUL", "nul.txt", "CON", "prn.dat", "aux", "com1", "lpt1"} {
		if !isReservedDOSDeviceName(name) {
			t.Fatalf("expected %q to be reserved DOS device name", name)
		}
	}
	for _, name := range []string{"made.txt", "null.txt", "config.json", "ppt.pptx"} {
		if isReservedDOSDeviceName(name) {
			t.Fatalf("expected %q NOT to be reserved DOS device name", name)
		}
	}
}
