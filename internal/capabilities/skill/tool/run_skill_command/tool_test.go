package run_skill_command

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
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
	binding := execmodel.ExecutionBinding{ID: "skill-binding", Mode: execmodel.WorkspaceModeSession, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyStrictWorkspace, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1", TaskID: "skill:demo"}}
	execution := workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: "/workspace/work/skill-binding"}}
	manifest := workmodel.RunManifest{RunID: "run-1", StateRoot: workmodel.StateRoot{ID: "state", Authority: "test"}, Executions: []workmodel.PreparedExecutionSnapshot{execution}}
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: manifest, Execution: execution})
	return workcontract.WithControlPlane(ctx, &skillTestControl{execution: execution, manifest: manifest})
}

func TestExecuteRequestsSessionWorkspaceBinding(t *testing.T) {
	binding := execmodel.ExecutionBinding{ID: "skill-binding", Mode: execmodel.WorkspaceModeSession, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyStrictWorkspace, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1", TaskID: "skill:demo"}}
	execution := workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: "/workspace/work/skill-binding"}}
	manifest := workmodel.RunManifest{RunID: "run-1", StateRoot: workmodel.StateRoot{ID: "state", Authority: "test"}, Executions: []workmodel.PreparedExecutionSnapshot{execution}}
	control := &skillTestControl{execution: execution, manifest: manifest}
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: manifest, Execution: execution})
	ctx = workcontract.WithControlPlane(ctx, control)
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
	if control.request.Intent.RequiredMode != execmodel.WorkspaceModeSession || control.request.Intent.NeedsPersistentRun {
		t.Fatalf("derived intent = %+v", control.request.Intent)
	}
}

type captureRunner struct{ request scriptcontract.RunRequest }

func (r *captureRunner) Run(_ context.Context, req scriptcontract.RunRequest) (*scriptcontract.RunResult, error) {
	r.request = req
	return &scriptcontract.RunResult{OK: true, Skill: req.Skill, Command: req.Command}, nil
}

type fakeInputResolver struct {
	refs   []workmodel.ResourceRef
	inputs []string
}

func (r *fakeInputResolver) ResolveInputs(_ context.Context, inputs []string) ([]workmodel.ResourceRef, error) {
	r.inputs = append([]string(nil), inputs...)
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
	return workmodel.InputManifest{RunID: req.Binding.Owner.RunID, BindingID: req.Binding.ID, Inputs: []workmodel.InputRef{{ID: "input-1", Name: "report.pdf", Size: 1, SHA256: strings.Repeat("0", 64), Source: req.Sources[0], StagedPath: "runs/run-1/input/input-1/report.pdf"}}}, nil
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
	skillBinding := execmodel.ExecutionBinding{ID: "skill-binding", Mode: execmodel.WorkspaceModeSession, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1", TaskID: "skill:demo"}}
	rootExecution := workmodel.PreparedExecutionSnapshot{Binding: rootBinding, Workspace: execmodel.ExecutionWorkspace{WorkDir: `D:\state\runs\run-1\work\root`}}
	skillExecution := workmodel.PreparedExecutionSnapshot{Binding: skillBinding, Workspace: execmodel.ExecutionWorkspace{WorkDir: `D:\state\runs\run-1\work\skill`}}
	manifest := workmodel.RunManifest{RunID: "run-1", StateRoot: workmodel.StateRoot{ID: "state", Authority: "test"}, Executions: []workmodel.PreparedExecutionSnapshot{rootExecution, skillExecution}}
	control := &skillTestControl{execution: skillExecution, manifest: manifest}
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: manifest, Execution: rootExecution})
	ctx = workcontract.WithControlPlane(ctx, control)

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

func TestAutoDetectReferencedInputs(t *testing.T) {
	tmpDir := t.TempDir()
	sampleFile := filepath.Join(tmpDir, "2026笔记本选型比较.pptx")
	if err := os.WriteFile(sampleFile, []byte("fake pptx"), 0o600); err != nil {
		t.Fatal(err)
	}

	ws := execmodel.ExecutionWorkspace{WorkDir: tmpDir}
	cmd := `cp "2026笔记本选型比较.pptx" "2026笔记本选型比较-编辑版.pptx"`
	inputs := autoDetectReferencedInputs(cmd, nil, ws)
	if len(inputs) != 1 || inputs[0] != "2026笔记本选型比较.pptx" {
		t.Fatalf("autoDetectReferencedInputs() = %v, want [2026笔记本选型比较.pptx]", inputs)
	}
}
