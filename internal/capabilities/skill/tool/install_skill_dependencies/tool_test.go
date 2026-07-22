package install_skill_dependencies

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type installTestControl struct {
	execution workmodel.PreparedExecutionSnapshot
}

func (c installTestControl) PrepareRun(context.Context, workcontract.PrepareRunRequest) (workmodel.PreparedRun, error) {
	return workmodel.PreparedRun{}, nil
}
func (c installTestControl) PrepareExecution(context.Context, workcontract.PrepareExecutionRequest) (workmodel.PreparedExecutionSnapshot, error) {
	return c.execution, nil
}
func (c installTestControl) GetRunManifest(context.Context, string, string) (workmodel.RunManifest, error) {
	return workmodel.RunManifest{}, nil
}

type allowAllApproval struct{}

func (allowAllApproval) Authorize(ctx context.Context, req approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved, Reason: "test"}, nil
}

type denyApproval struct{}

func (denyApproval) Authorize(ctx context.Context, req approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionDenied, Reason: "user denied"}, nil
}

type fakeRunner struct {
	lastCmd  execmodel.Command
	lastOpts execcontract.RunOptions
}

func (f *fakeRunner) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	f.lastCmd = cmd
	f.lastOpts = opts
	return &execmodel.Result{ExitCode: 0, Stdout: "ok"}, nil
}

type recordingRunner struct {
	cmds []execmodel.Command
}

func (r *recordingRunner) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	r.cmds = append(r.cmds, cmd)
	return &execmodel.Result{ExitCode: 0, Stdout: "ok"}, nil
}

type fakeSkills struct {
	meta skillmodel.ResolvedInvocation
}

func (f fakeSkills) Resolve(context.Context, skillcontract.ResolveRequest) (skillmodel.ResolvedInvocation, error) {
	return f.meta, nil
}

func testMeta(runtime skillmodel.RuntimeDeps) skillmodel.ResolvedInvocation {
	meta := skillmodel.Metadata{Name: "office-ppt", PackageID: "office-ppt", MainResource: "office-ppt/SKILL.md"}.Normalize()
	return skillmodel.ResolvedInvocation{
		CatalogItem: skillmodel.InvocationMetadata{Name: "office-ppt", QualifiedName: "office-ppt", PhysicalSkill: "office-ppt", PackageID: "office-ppt"},
		Physical:    skillmodel.PhysicalSkillDefinition{Metadata: meta},
		Definition:  skillmodel.InvocationDefinition{ID: "work", Handle: "office-ppt", RuntimeProfile: "work"},
		Profile:     skillmodel.RuntimeProfile{Dependencies: skillmodel.Dependencies{Runtime: runtime}},
	}
}

func TestInstallRejectsPackageNotInWhitelist(t *testing.T) {
	tool, err := New(Deps{
		Skills:   fakeSkills{meta: testMeta(skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs"}}})},
		Runner:   &fakeRunner{},
		Approval: allowAllApproval{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tool.Execute(installTestContext(), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"left-pad"}]}`)
	if err == nil || !strings.Contains(err.Error(), "未在该 skill 的 dependencies.runtime 中声明") {
		t.Fatalf("err=%v", err)
	}
}

func TestInstallRejectsWhenNoRuntimeDeclared(t *testing.T) {
	tool, err := New(Deps{
		Skills:   fakeSkills{meta: testMeta(skillmodel.RuntimeDeps{})},
		Runner:   &fakeRunner{},
		Approval: allowAllApproval{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tool.Execute(installTestContextWithRuntime(skillmodel.RuntimeDeps{}), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}]}`)
	if err == nil || !strings.Contains(err.Error(), "未声明") {
		t.Fatalf("err=%v", err)
	}
}

func TestInstallDeniedByApproval(t *testing.T) {
	tool, err := New(Deps{
		Skills:        fakeSkills{meta: testMeta(skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs"}}})},
		Runner:        &fakeRunner{},
		Approval:      denyApproval{},
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := tool.Execute(installTestContext(), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}]}`)
	if err == nil {
		t.Fatal("expected approval error")
	}
	if !strings.Contains(body, `"ok":false`) || !strings.Contains(body, "approval_denied") {
		t.Fatalf("body=%s err=%v", body, err)
	}
}

func TestInstallSuccessRunsWhitelistedCommand(t *testing.T) {
	runner := &fakeRunner{}
	root := t.TempDir()
	tool, err := New(Deps{
		Skills:        fakeSkills{meta: testMeta(skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs"}}})},
		Runner:        runner,
		Approval:      allowAllApproval{},
		WorkspaceRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := tool.Execute(installTestContext(root), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}],"scope":"workspace"}`)
	if err != nil {
		t.Fatalf("Execute: %v body=%s", err, body)
	}
	var payload resultPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Scope != "workspace" {
		t.Fatalf("payload=%+v", payload)
	}
	installRoot := filepath.Join(root, ".genesis", "cache", "skill-deps", "office-ppt")
	wantCwd := filepath.Join(installRoot, "node")
	wantCommand := "npm install pptxgenjs"
	if runner.lastCmd.Command != wantCommand {
		t.Fatalf("command=%q want=%q", runner.lastCmd.Command, wantCommand)
	}
	if runner.lastCmd.Cwd != wantCwd {
		t.Fatalf("cwd=%q want=%q", runner.lastCmd.Cwd, wantCwd)
	}
	if runner.lastOpts.Workspace.WorkDir != root {
		t.Fatalf("workspace=%+v want workdir=%s", runner.lastOpts.Workspace, root)
	}
	if runner.lastOpts.Sandbox.Operation != execmodel.SandboxOperationBuildDependencies {
		t.Fatalf("operation=%s", runner.lastOpts.Sandbox.Operation)
	}
	if runner.lastOpts.Sandbox.RuntimeProfile != execmodel.RuntimeProfileSkillBuildPolyglot {
		t.Fatalf("profile=%s", runner.lastOpts.Sandbox.RuntimeProfile)
	}
	if runner.lastOpts.Sandbox.Metadata["skill_dep_install"] != "true" {
		t.Fatalf("metadata=%v", runner.lastOpts.Sandbox.Metadata)
	}
	if payload.Metadata["install_root"] != installRoot {
		t.Fatalf("metadata install_root=%q want=%q", payload.Metadata["install_root"], installRoot)
	}
}

func TestBuildInstallStepsAvoidsQuotedAbsolutePrefix(t *testing.T) {
	// 模拟 Windows 绝对路径字符串（任意 OS 可断言），禁止回归到 --prefix "D:\..." 形态。
	installRoot := `D:\workspace\go\genesis-agent\.genesis\cache\skill-deps\office-ppt`
	steps, err := buildInstallSteps(
		[]packageInput{
			{Manager: "npm", Name: "pptxgenjs"},
			{Manager: "pip", Name: "markitdown[pptx]"},
		},
		installRoot,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) < 2 {
		t.Fatalf("steps=%d %+v", len(steps), steps)
	}
	npm := steps[0]
	if npm.Command != "npm install pptxgenjs" {
		t.Fatalf("npm command=%q", npm.Command)
	}
	if npm.Cwd != filepath.Join(installRoot, "node") {
		t.Fatalf("npm cwd=%q", npm.Cwd)
	}
	if strings.Contains(npm.Command, "--prefix") || strings.Contains(npm.Command, `"`) {
		t.Fatalf("npm command must not use quoted --prefix: %q", npm.Command)
	}
	pip := steps[len(steps)-1]
	wantPip := venvPythonRel() + " -m pip install " + quotePipPackageArg("markitdown[pptx]")
	if pip.Command != wantPip {
		t.Fatalf("pip command=%q want=%q", pip.Command, wantPip)
	}
	if pip.Cwd != filepath.Join(installRoot, "venv") {
		t.Fatalf("pip cwd=%q", pip.Cwd)
	}
	if strings.Contains(pip.Command, "--target") || strings.Contains(pip.Command, installRoot) {
		t.Fatalf("pip command must use venv relative python without abs path: %q", pip.Command)
	}
	if runtime.GOOS == "windows" && strings.Contains(pip.Command, `"`) {
		t.Fatalf("windows pip extras must not be double-quoted (cmd keeps quotes): %q", pip.Command)
	}
}

func TestInstallPipUsesVenv(t *testing.T) {
	runner := &recordingRunner{}
	root := t.TempDir()
	tool, err := New(Deps{
		Skills: fakeSkills{meta: testMeta(skillmodel.RuntimeDeps{
			Python: []skillmodel.RuntimePackage{{Name: "markitdown"}},
		})},
		Runner:        runner,
		Approval:      allowAllApproval{},
		WorkspaceRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := tool.Execute(installTestContext(root), `{"skill":"office-ppt","packages":[{"manager":"pip","name":"markitdown[pptx]"}],"scope":"workspace"}`)
	if err != nil {
		t.Fatalf("Execute: %v body=%s", err, body)
	}
	if len(runner.cmds) < 2 {
		t.Fatalf("cmds=%+v", runner.cmds)
	}
	installRoot := filepath.Join(root, ".genesis", "cache", "skill-deps", "office-ppt")
	if runner.cmds[0].Command != "python -m venv venv" || runner.cmds[0].Cwd != installRoot {
		t.Fatalf("venv step=%+v", runner.cmds[0])
	}
	wantCwd := filepath.Join(installRoot, "venv")
	wantCmd := venvPythonRel() + " -m pip install " + quotePipPackageArg("markitdown[pptx]")
	last := runner.cmds[len(runner.cmds)-1]
	if last.Command != wantCmd || last.Cwd != wantCwd {
		t.Fatalf("pip step=%+v want cmd=%q cwd=%q", last, wantCmd, wantCwd)
	}
}

func TestNormalizeAllowsPipExtrasAgainstBaseWhitelist(t *testing.T) {
	pkgs, err := normalizeAndAuthorizePackages(
		[]packageInput{{Manager: "pip", Name: "markitdown[pptx]"}},
		skillmodel.Dependencies{Runtime: skillmodel.RuntimeDeps{Python: []skillmodel.RuntimePackage{{Name: "markitdown"}}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "markitdown[pptx]" {
		t.Fatalf("pkgs=%+v", pkgs)
	}
}

func TestInstallWorkspaceScopeRejectedForRemoteGenesisSandbox(t *testing.T) {
	runner := &fakeRunner{}
	tool, err := New(Deps{
		Skills:        fakeSkills{meta: testMeta(skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs"}}})},
		Runner:        runner,
		Approval:      allowAllApproval{},
		WorkspaceRoot: t.TempDir(),
		Sandbox:       execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := tool.Execute(installTestContext(), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}],"scope":"workspace"}`)
	if err == nil {
		t.Fatal("expected remote workspace install visibility error")
	}
	if !strings.Contains(body, "install_scope_not_visible") {
		t.Fatalf("body=%s", body)
	}
	if runner.lastCmd.Command != "" {
		t.Fatalf("runner should not be called, got %q", runner.lastCmd.Command)
	}
}

func TestInstallImageScopeForbidden(t *testing.T) {
	tool, err := New(Deps{
		Skills:   fakeSkills{meta: testMeta(skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs"}}})},
		Runner:   &fakeRunner{},
		Approval: allowAllApproval{},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := tool.Execute(installTestContext(), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}],"scope":"image"}`)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(body, "install_forbidden_use_image") {
		t.Fatalf("body=%s", body)
	}
}

func TestInstallSessionScopeUnsupportedInGateB(t *testing.T) {
	tool, err := New(Deps{
		Skills:   fakeSkills{meta: testMeta(skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs"}}})},
		Runner:   &fakeRunner{},
		Approval: allowAllApproval{},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := tool.Execute(installTestContext(), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}],"scope":"session"}`)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(body, "install_scope_unsupported") {
		t.Fatalf("body=%s", body)
	}
}

func TestRejectUnsafePackageName(t *testing.T) {
	_, err := normalizeAndAuthorizePackages(
		[]packageInput{{Manager: "npm", Name: "pptxgenjs; rm -rf /"}},
		skillmodel.Dependencies{Runtime: skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs; rm -rf /"}}}},
	)
	if err == nil {
		t.Fatal("expected unsafe name reject")
	}
	_, err = normalizeAndAuthorizePackages(
		[]packageInput{{Manager: "npm", Name: "../evil"}},
		skillmodel.Dependencies{Runtime: skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "../evil"}}}},
	)
	if err == nil {
		t.Fatal("expected path traversal reject")
	}
}

func installTestContext(roots ...string) context.Context {
	return installTestContextWithRuntime(skillmodel.RuntimeDeps{
		Node:   []skillmodel.RuntimePackage{{Name: "pptxgenjs"}},
		Python: []skillmodel.RuntimePackage{{Name: "markitdown"}},
	}, roots...)
}

func installTestContextWithRuntime(runtimeDeps skillmodel.RuntimeDeps, roots ...string) context.Context {
	workspaceRoot := "test-project"
	if len(roots) > 0 {
		workspaceRoot = roots[0]
	}
	binding := execmodel.ExecutionBinding{ID: "skill-deps-binding", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyPermissionOnly, Owner: execmodel.ExecutionOwnerRef{RunID: "run-install-test", TaskID: "skill-deps:office-ppt"}}
	execution := workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: workspaceRoot}}
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: workmodel.RunManifest{RunID: "run-install-test"}, Execution: execution})
	ctx = workcontract.WithControlPlane(ctx, installTestControl{execution: execution})
	invocation := skillmodel.InvocationBinding{
		ID: "invocation-install", RunID: "run-install-test", InvocationID: "work", Handle: "office-ppt", PhysicalSkill: "office-ppt",
		Package:   skillmodel.SkillPackageSnapshot{Authority: skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, PackageID: "office-ppt", Digest: strings.Repeat("a", 64)},
		AgentMode: skillmodel.AgentModeSpec{Mode: skillmodel.AgentModeMain}, RuntimeProfile: skillmodel.RuntimeProfile{Dependencies: skillmodel.Dependencies{Runtime: runtimeDeps}},
		IdempotencyKey: "install-test", CreatedAt: time.Now(),
	}
	return skillcontract.WithInvocationBinding(ctx, invocation)
}
