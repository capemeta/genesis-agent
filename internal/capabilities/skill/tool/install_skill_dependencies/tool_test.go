package install_skill_dependencies

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
)

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

type fakeSkills struct {
	meta skillmodel.Metadata
}

func (f fakeSkills) Resolve(context.Context, skillcontract.ResolveRequest) (skillmodel.Metadata, error) {
	return f.meta, nil
}

func testMeta(runtime skillmodel.RuntimeDeps) skillmodel.Metadata {
	return skillmodel.Metadata{
		Name:          "office-ppt",
		QualifiedName: "office-ppt",
		PackageID:     "office-ppt",
		Enabled:       true,
		Dependencies: skillmodel.Dependencies{
			Runtime: runtime,
		},
	}.Normalize()
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
	_, err = tool.Execute(context.Background(), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"left-pad"}]}`)
	if err == nil || !strings.Contains(err.Error(), "白名单") {
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
	_, err = tool.Execute(context.Background(), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}]}`)
	if err == nil || !strings.Contains(err.Error(), "未声明") {
		t.Fatalf("err=%v", err)
	}
}

func TestInstallDeniedByApproval(t *testing.T) {
	tool, err := New(Deps{
		Skills:   fakeSkills{meta: testMeta(skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs"}}})},
		Runner:   &fakeRunner{},
		Approval: denyApproval{},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := tool.Execute(context.Background(), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}]}`)
	if err == nil {
		t.Fatal("expected approval error")
	}
	if !strings.Contains(body, `"ok":false`) || !strings.Contains(body, "approval_denied") {
		t.Fatalf("body=%s err=%v", body, err)
	}
}

func TestInstallSuccessRunsWhitelistedCommand(t *testing.T) {
	runner := &fakeRunner{}
	tool, err := New(Deps{
		Skills:        fakeSkills{meta: testMeta(skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs"}}})},
		Runner:        runner,
		Approval:      allowAllApproval{},
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := tool.Execute(context.Background(), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}],"scope":"workspace"}`)
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
	if runner.lastCmd.Command != "npm install pptxgenjs" {
		t.Fatalf("command=%q", runner.lastCmd.Command)
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
	body, err := tool.Execute(context.Background(), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}],"scope":"image"}`)
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
	body, err := tool.Execute(context.Background(), `{"skill":"office-ppt","packages":[{"manager":"npm","name":"pptxgenjs"}],"scope":"session"}`)
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
