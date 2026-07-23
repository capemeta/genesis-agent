package run_skill_command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type skillTestControl struct {
	execution workmodel.PreparedExecutionSnapshot
	manifest  workmodel.RunManifest
	request   workcontract.PrepareExecutionRequest
}

func (s skillTestControl) PrepareRun(context.Context, workcontract.PrepareRunRequest) (workmodel.PreparedRun, error) {
	return workmodel.PreparedRun{}, nil
}
func (s *skillTestControl) PrepareExecution(_ context.Context, req workcontract.PrepareExecutionRequest) (workmodel.PreparedExecutionSnapshot, error) {
	s.request = req
	return s.execution, nil
}
func (s skillTestControl) GetRunManifest(context.Context, string, string) (workmodel.RunManifest, error) {
	return s.manifest, nil
}

func skillTestContext() context.Context {
	binding := execmodel.ExecutionBinding{ID: "skill-binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyStrictWorkspace, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1", TaskID: "skill:demo"}}
	execution := workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: "/workspace/work/skill-binding"}}
	manifest := workmodel.RunManifest{RunID: "run-1", StateRoot: workmodel.StateRoot{ID: "state", Authority: "test"}, Executions: []workmodel.PreparedExecutionSnapshot{execution}}
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: manifest, Execution: execution})
	return withInvocation(workcontract.WithControlPlane(ctx, &skillTestControl{execution: execution, manifest: manifest}))
}

func withInvocation(ctx context.Context) context.Context {
	return skillcontract.WithInvocationBinding(ctx, skillmodel.InvocationBinding{
		ID: "invocation-binding", RunID: "run-1", Handle: "demo", PhysicalSkill: "demo", InvocationID: "work",
		Package: skillmodel.SkillPackageSnapshot{Digest: "sha256:package"}, IdempotencyKey: "invocation-key",
		ExecutionPolicy: skillmodel.EffectiveExecutionPolicy{SandboxRequired: false, ExecutionMode: skillmodel.ExecutionModePerCall},
	})
}

func TestExecuteRequestsTaskWorkspaceBinding(t *testing.T) {
	binding := execmodel.ExecutionBinding{ID: "skill-binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyStrictWorkspace, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1", TaskID: "skill:demo"}}
	execution := workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: "/workspace/work/skill-binding"}}
	manifest := workmodel.RunManifest{RunID: "run-1", StateRoot: workmodel.StateRoot{ID: "state", Authority: "test"}, Executions: []workmodel.PreparedExecutionSnapshot{execution}}
	control := &skillTestControl{execution: execution, manifest: manifest}
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: manifest, Execution: execution})
	ctx = workcontract.WithControlPlane(ctx, control)
	ctx = withInvocation(ctx)
	tool, err := New(Deps{Runner: &captureRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(ctx, `{"skill":"demo","command":"python script.py"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"duration_ms":`, `"approval_duration_ms":0`, `"staging_duration_ms":0`, `"execution_duration_ms":0`} {
		if !strings.Contains(result, field) {
			t.Fatalf("零值分阶段耗时不得省略 %s: %s", field, result)
		}
	}
	if control.request.Intent.RequiredMode != execmodel.WorkspaceModeTask || control.request.Intent.NeedsPersistentRun {
		t.Fatalf("derived intent = %+v", control.request.Intent)
	}
}

type captureRunner struct{ request scriptcontract.RunRequest }

func (r *captureRunner) Run(_ context.Context, req scriptcontract.RunRequest) (*scriptcontract.RunResult, error) {
	r.request = req
	return &scriptcontract.RunResult{OK: true, Skill: req.Skill, Command: req.Command}, nil
}

type fakeInputResolver struct {
	refs           []workmodel.ResourceRef
	inputs         []string
	optionalInputs []string
}

func (r *fakeInputResolver) ResolveInputs(_ context.Context, inputs []string) ([]workmodel.ResourceRef, error) {
	r.inputs = append([]string(nil), inputs...)
	for _, in := range inputs {
		if strings.Contains(in, "non_existent") {
			return nil, fmt.Errorf("resource not found: %s", in)
		}
	}
	return append([]workmodel.ResourceRef(nil), r.refs...), nil
}

func (r *fakeInputResolver) ResolveAvailableInputs(_ context.Context, inputs []string) ([]workmodel.ResourceRef, error) {
	r.optionalInputs = append([]string(nil), inputs...)
	return append([]workmodel.ResourceRef(nil), r.refs...), nil
}

type fakeInputStager struct {
	request workcontract.StageRequest
	delay   time.Duration
}

func (s *fakeInputStager) Stage(_ context.Context, req workcontract.StageRequest) (workmodel.InputManifest, error) {
	s.request = req
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return workmodel.InputManifest{RunID: req.Binding.Owner.RunID, BindingID: req.Binding.ID, Inputs: []workmodel.InputRef{{ID: "input-1", Name: "report.pdf", Alias: "report.pdf", Size: 1, SHA256: strings.Repeat("0", 64), Source: req.Sources[0], StagedPath: "runs/run-1/input/input-1/report.pdf"}}}, nil
}

func TestExecuteReportsWholeHarnessAndControlPlaneStagingTime(t *testing.T) {
	runner := &captureRunner{}
	stager := &fakeInputStager{delay: 10 * time.Millisecond}
	resolver := &fakeInputResolver{refs: []workmodel.ResourceRef{{Authority: "host", Scheme: "file", ID: "resource-1", Version: "v1"}}}
	tool, err := New(Deps{Runner: runner, InputResolver: resolver, InputStager: stager})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(skillTestContext(), `{"skill":"demo","command":"python script.py","inputs":["report.pdf"]}`)
	if err != nil {
		t.Fatal(err)
	}
	var payload scriptcontract.RunResult
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DurationMS < 10 || payload.StagingDurationMS < 10 || payload.DurationMS < payload.StagingDurationMS {
		t.Fatalf("harness timing=%+v", payload)
	}
}

func TestExecuteStagesResourceRefsBeforeRunner(t *testing.T) {
	runner := &captureRunner{}
	stager := &fakeInputStager{}
	ref := workmodel.ResourceRef{Authority: "host", Scheme: "file", ID: "resource-1", Version: "v1", Path: "D:/docs/report.pdf"}
	resolver := &fakeInputResolver{refs: []workmodel.ResourceRef{ref}}
	tool, err := New(Deps{Runner: runner, InputResolver: resolver, InputStager: stager, Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := skillTestContext()
	if _, err := tool.Execute(ctx, `{"skill":"demo","command":"python script.py","inputs":["D:/docs/report.pdf"]}`); err != nil {
		t.Fatal(err)
	}
	if len(stager.request.Sources) != 1 || stager.request.Sources[0].ID != "resource-1" {
		t.Fatalf("stager 未收到稳定 ResourceRef: %+v", stager.request)
	}
	if len(runner.request.Inputs.Inputs) != 1 || runner.request.Inputs.BindingID != runner.request.Binding.ID {
		t.Fatalf("runner 未收到绑定后的 InputManifest: %+v", runner.request.Inputs)
	}
}

func TestExecuteExpandsCallerWorkDirBeforeResolvingInput(t *testing.T) {
	runner := &captureRunner{}
	stager := &fakeInputStager{}
	ref := workmodel.ResourceRef{Authority: "host", Scheme: "file", ID: "script-1", Version: "v1", Path: "D:/state/runs/run-1/work/root/create.js"}
	resolver := &fakeInputResolver{refs: []workmodel.ResourceRef{ref}}
	tool, err := New(Deps{Runner: runner, InputResolver: resolver, InputStager: stager})
	if err != nil {
		t.Fatal(err)
	}
	rootBinding := execmodel.ExecutionBinding{ID: "root-binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1"}}
	skillBinding := execmodel.ExecutionBinding{ID: "skill-binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1", TaskID: "skill:demo"}}
	rootExecution := workmodel.PreparedExecutionSnapshot{Binding: rootBinding, Workspace: execmodel.ExecutionWorkspace{WorkDir: `D:\state\runs\run-1\work\root`}}
	skillExecution := workmodel.PreparedExecutionSnapshot{Binding: skillBinding, Workspace: execmodel.ExecutionWorkspace{WorkDir: `D:\state\runs\run-1\work\skill`}}
	manifest := workmodel.RunManifest{RunID: "run-1", StateRoot: workmodel.StateRoot{ID: "state", Authority: "test"}, Executions: []workmodel.PreparedExecutionSnapshot{rootExecution, skillExecution}}
	control := &skillTestControl{execution: skillExecution, manifest: manifest}
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: manifest, Execution: rootExecution})
	ctx = workcontract.WithControlPlane(ctx, control)
	ctx = withInvocation(ctx)

	if _, err := tool.Execute(ctx, `{"skill":"demo","command":"node create.js","inputs":["$WORK_DIR/create.js"]}`); err != nil {
		t.Fatal(err)
	}
	want := `D:\state\runs\run-1\work\root\create.js`
	if len(resolver.inputs) != 1 || resolver.inputs[0] != want {
		t.Fatalf("resolver inputs=%q want=%q", resolver.inputs, want)
	}
}

func TestExecuteRejectsRawInputsWithoutControlPlaneStager(t *testing.T) {
	tool, err := New(Deps{Runner: &captureRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := skillTestContext()
	_, err = tool.Execute(ctx, `{"skill":"demo","command":"python script.py","inputs":["report.pdf"]}`)
	if err == nil || !strings.Contains(err.Error(), "INPUT_PERMISSION_DENIED") {
		t.Fatalf("缺少控制面 stager 时必须 fail-closed: %v", err)
	}
}

func TestApplyFinalizationKeepsOKOnDeliveryConflict(t *testing.T) {
	result := &scriptcontract.RunResult{OK: true}
	applyFinalization(result, artifactmodel.FinalizationResult{
		Resolutions: []artifactmodel.DeliverableResolution{{
			DeliverableID: "deck",
			Status:        "delivery_conflict",
			Warning:       "DELIVERY_TARGET_CONFLICT: deliverable deck 目标无法覆盖交付（非普通文件或权限拒绝）",
		}},
	})
	if !result.OK || result.FailureKind != "" || len(result.Warnings) != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestExecuteAutomaticallyRecordsOptionalVisionDegradeBeforeDelivery(t *testing.T) {
	ctx := skillTestContext()
	binding, ok := skillcontract.InvocationBindingFromContext(ctx)
	if !ok {
		t.Fatal("missing invocation binding")
	}
	binding.Capabilities.VisionMode = "degraded_text"
	binding.Result = skillmodel.ResultContract{Kind: skillmodel.ResultKindDeliverables, Deliverables: []skillmodel.DeliverableDeclaration{{
		ID: "deck", Role: skillmodel.DeliverableRolePrimary, Required: true, Cardinality: skillmodel.DeliverableExactlyOne,
		QA: skillmodel.QADeclaration{Policy: "visual-qa/v1", Enforcement: "optional"},
	}}}
	ctx = skillcontract.WithInvocationBinding(ctx, binding)
	finalizer := &sequencedFinalizer{}
	recorder := &recordingQADegrade{}
	created, err := New(Deps{Runner: &captureRunner{}, Finalizer: finalizer, QAEvidence: recorder})
	if err != nil {
		t.Fatal(err)
	}
	result, err := created.Execute(ctx, `{"skill":"demo","command":"python script.py"}`)
	if err != nil {
		t.Fatal(err)
	}
	if finalizer.calls != 2 || recorder.calls != 1 || recorder.request.FailureCode != "vision_unavailable" {
		t.Fatalf("finalizer=%d recorder=%d request=%+v", finalizer.calls, recorder.calls, recorder.request)
	}
	if !strings.Contains(result, "已由 Harness 发布并交付") {
		t.Fatalf("expected delivered result after automatic degrade: %s", result)
	}
}

type sequencedFinalizer struct{ calls int }

func (f *sequencedFinalizer) FinalizeRequired(context.Context, string, string) (artifactmodel.FinalizationResult, error) {
	f.calls++
	status := "qa_pending"
	if f.calls > 1 {
		status = "delivered"
	}
	return artifactmodel.FinalizationResult{Resolutions: []artifactmodel.DeliverableResolution{{DeliverableID: "deck", Status: status}}}, nil
}

func (f *sequencedFinalizer) SelectAndFinalize(context.Context, string, string, string, string) (artifactmodel.DeliveryResult, error) {
	return artifactmodel.DeliveryResult{}, nil
}

type recordingQADegrade struct {
	calls   int
	request artifactcontract.QAOutcomeRequest
}

func (r *recordingQADegrade) RecordOutcome(_ context.Context, request artifactcontract.QAOutcomeRequest) error {
	r.calls++
	r.request = request
	return nil
}

func TestCollectWorkspaceInputsIncludesBoundInputAndCommandEntry(t *testing.T) {
	view := workmodel.WorkspaceViewManifest{Entries: []workmodel.WorkspaceViewEntry{{Path: "2026笔记本选型比较.pptx"}}}
	required, optional := collectWorkspaceInputs(`node create.js "2026笔记本选型比较.pptx"`, []string{"notes.json"}, view)
	wantRequired := []string{"2026笔记本选型比较.pptx", "notes.json"}
	if strings.Join(required, "|") != strings.Join(wantRequired, "|") {
		t.Fatalf("collectWorkspaceInputs() required = %v, want %v", required, wantRequired)
	}
	if len(optional) != 1 || optional[0] != "create.js" {
		t.Fatalf("collectWorkspaceInputs() optional = %v", optional)
	}
}

func TestCollectWorkspaceInputsFiltersShellRedirectionAndOutputFlags(t *testing.T) {
	view := workmodel.WorkspaceViewManifest{}
	_, optionalRedirection := collectWorkspaceInputs(`python process.py input.csv > aaa.txt`, nil, view)
	for _, opt := range optionalRedirection {
		if opt == "aaa.txt" {
			t.Fatalf("shell output redirection 目标 aaa.txt 不可以加入 optional: %v", optionalRedirection)
		}
	}

	_, optionalFlag := collectWorkspaceInputs(`pandoc in.md -o out.pdf`, nil, view)
	if len(optionalFlag) != 1 || optionalFlag[0] != "in.md" {
		t.Fatalf("CLI -o 输出标志目标不可以加入 optional, expected in.md, got: %v", optionalFlag)
	}

	_, optionalOutDir := collectWorkspaceInputs(`soffice --convert-to pdf --outdir ./out in.docx`, nil, view)
	if len(optionalOutDir) != 1 || optionalOutDir[0] != "in.docx" {
		t.Fatalf("CLI --outdir 输出标志目标不可以加入 optional, expected in.docx, got: %v", optionalOutDir)
	}
}

func TestExecutePhysicalStat3StepInputResolution(t *testing.T) {
	tempSkillDir := t.TempDir()
	tempParentDir := t.TempDir()

	// 准备 Skill Task Workspace 本地产物
	localFile := filepath.Join(tempSkillDir, "local_created.pptx")
	if err := os.WriteFile(localFile, []byte("ppt data"), 0644); err != nil {
		t.Fatal(err)
	}

	// 准备 Parent Run Workspace 外部数据
	parentFile := filepath.Join(tempParentDir, "sales_data.xlsx")
	if err := os.WriteFile(parentFile, []byte("excel data"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := &captureRunner{}
	stager := &fakeInputStager{}
	ref := workmodel.ResourceRef{Authority: "host", Scheme: "file", ID: "res-parent", Version: "v1", Path: parentFile}
	resolver := &fakeInputResolver{refs: []workmodel.ResourceRef{ref}}

	tool, err := New(Deps{Runner: runner, InputResolver: resolver, InputStager: stager})
	if err != nil {
		t.Fatal(err)
	}

	skillBinding := execmodel.ExecutionBinding{ID: "skill-binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1", TaskID: "skill:demo"}}
	rootExecution := workmodel.PreparedExecutionSnapshot{Binding: execmodel.ExecutionBinding{ID: "root-binding"}, Workspace: execmodel.ExecutionWorkspace{WorkDir: tempParentDir}}
	skillExecution := workmodel.PreparedExecutionSnapshot{Binding: skillBinding, Workspace: execmodel.ExecutionWorkspace{WorkDir: tempSkillDir}}
	manifest := workmodel.RunManifest{RunID: "run-1", StateRoot: workmodel.StateRoot{ID: "state", Authority: "test"}, Executions: []workmodel.PreparedExecutionSnapshot{rootExecution, skillExecution}}

	control := &skillTestControl{execution: skillExecution, manifest: manifest}
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: manifest, Execution: rootExecution})
	ctx = workcontract.WithControlPlane(ctx, control)
	ctx = withInvocation(ctx)

	// 测试 Step 1：Skill 本地已包含 local_created.pptx -> 放行且不请求 parent staging
	if _, err := tool.Execute(ctx, `{"skill":"demo","command":"python process.py","inputs":["local_created.pptx"]}`); err != nil {
		t.Fatalf("Step 1 物理 Stat 命中放行失败: %v", err)
	}

	// 测试 Step 2：Skill 本地没有，Parent 有 sales_data.xlsx -> 触发 parent staging
	if _, err := tool.Execute(ctx, `{"skill":"demo","command":"python process.py","inputs":["sales_data.xlsx"]}`); err != nil {
		t.Fatalf("Step 2 主工作区 Stat 命中 Staging 失败: %v", err)
	}

	// 测试 Step 3：两侧均不存在 non_existent.csv -> 报明确错误
	_, err = tool.Execute(ctx, `{"skill":"demo","command":"python process.py","inputs":["non_existent.csv"]}`)
	if err == nil || !strings.Contains(err.Error(), "均不存在") {
		t.Fatalf("Step 3 两侧不存在必须抛出明确错误: %v", err)
	}
}

func TestTrustedExecutionBackendRecognizesCanonicalLocalPlatformProvider(t *testing.T) {
	backend := trustedExecutionBackend(execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "local-platform"})
	if backend.Kind != execmodel.BackendKindLocalSandbox || backend.Authority != "host" {
		t.Fatalf("backend=%+v", backend)
	}
}
