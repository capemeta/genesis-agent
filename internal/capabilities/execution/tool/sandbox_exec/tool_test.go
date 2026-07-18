package sandbox_exec

import (
	"context"
	"encoding/json"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	execservice "genesis-agent/internal/capabilities/execution/service"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type execToolControl struct {
	request workcontract.PrepareExecutionRequest
	result  workmodel.PreparedExecutionSnapshot
}

func (*execToolControl) PrepareRun(context.Context, workcontract.PrepareRunRequest) (workmodel.PreparedRun, error) {
	return workmodel.PreparedRun{}, nil
}
func (c *execToolControl) PrepareExecution(_ context.Context, req workcontract.PrepareExecutionRequest) (workmodel.PreparedExecutionSnapshot, error) {
	c.request = req
	return c.result, nil
}
func (*execToolControl) GetRunManifest(context.Context, string, string) (workmodel.RunManifest, error) {
	return workmodel.RunManifest{}, nil
}

type execToolApproval struct{ request approvalmodel.Request }

func (a *execToolApproval) Authorize(_ context.Context, req approvalmodel.Request) (approvalmodel.Decision, error) {
	a.request = req
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

type execToolResolver struct{ inputs []string }

func (r *execToolResolver) ResolveInputs(_ context.Context, inputs []string) ([]workmodel.ResourceRef, error) {
	r.inputs = append([]string(nil), inputs...)
	refs := make([]workmodel.ResourceRef, len(inputs))
	return refs, nil
}

type execToolStager struct{ request workcontract.StageRequest }

func (s *execToolStager) Stage(_ context.Context, req workcontract.StageRequest) (workmodel.InputManifest, error) {
	s.request = req
	return workmodel.InputManifest{RunID: req.Binding.Owner.RunID, BindingID: req.Binding.ID}, nil
}

type execToolRunner struct {
	request execservice.SandboxCommandRequest
}

func (r *execToolRunner) RunSandboxCommand(_ context.Context, req execservice.SandboxCommandRequest) (*execservice.SandboxCommandResult, error) {
	r.request = req
	return &execservice.SandboxCommandResult{Result: &execmodel.Result{ExitCode: 0, Stdout: "ok"}, Workspace: req.Workspace, StagedInputs: []string{"bound.txt", "extra.txt"}}, nil
}

func TestSandboxExecDerivesRemoteExecutionAndStagesDeclaredInputs(t *testing.T) {
	binding := execmodel.ExecutionBinding{ID: "sandbox-binding", Mode: execmodel.WorkspaceModeSession, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", RunID: "run", TaskID: "sandbox:command"}}
	workspace := execmodel.ExecutionWorkspace{WorkDir: "/workspace/work", InputDir: "/workspace/input/sandbox-binding", OutputDir: "/workspace/output/sandbox-binding", TmpDir: "/workspace/tmp/sandbox-binding"}
	control := &execToolControl{result: workmodel.PreparedExecutionSnapshot{Binding: binding, Backend: execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindRemote, Provider: "genesis-sandbox", Authority: "remote-executor"}, Workspace: workspace}}
	approval := &execToolApproval{}
	resolver := &execToolResolver{}
	stager := &execToolStager{}
	runner := &execToolRunner{}
	tool, err := New(Deps{Runner: runner, InputResolver: resolver, InputStager: stager, Approval: approval, Sandbox: execmodel.SandboxProfile{Provider: "genesis-sandbox"}})
	if err != nil {
		t.Fatal(err)
	}
	rootBinding := execmodel.ExecutionBinding{ID: "root", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run"}}
	prepared := workmodel.PreparedRun{Manifest: workmodel.RunManifest{RunID: "run", View: workmodel.WorkspaceViewManifest{Entries: []workmodel.WorkspaceViewEntry{{Path: "bound.txt"}}}}, Execution: workmodel.PreparedExecutionSnapshot{Binding: rootBinding, Workspace: execmodel.ExecutionWorkspace{WorkDir: "."}}}
	ctx := workcontract.WithPreparedRun(context.Background(), prepared)
	ctx = workcontract.WithControlPlane(ctx, control)
	out, err := tool.Execute(ctx, `{"command":"python build.py","inputs":["extra.txt","bound.txt"],"timeout_ms":9999999}`)
	if err != nil {
		t.Fatal(err)
	}
	if control.request.Backend.Kind != execmodel.BackendKindRemote || control.request.Subject.TaskID != "sandbox:command" || control.request.Intent.RequiredMode != execmodel.WorkspaceModeSession || !control.request.Intent.NeedsPersistentRun || !control.request.Intent.BoundedInputs || !control.request.Intent.BoundedOutputs {
		t.Fatalf("prepare request = %+v", control.request)
	}
	if len(resolver.inputs) != 2 || resolver.inputs[0] != "bound.txt" || resolver.inputs[1] != "extra.txt" {
		t.Fatalf("resolved inputs = %v", resolver.inputs)
	}
	if stager.request.Binding.ID != binding.ID || runner.request.Binding.ID != binding.ID || runner.request.Timeout != maxTimeout {
		t.Fatalf("stage=%+v run=%+v", stager.request, runner.request)
	}
	if runner.request.Sandbox.Mode != execmodel.SandboxRequired || runner.request.Command.Cwd != "" {
		t.Fatalf("sandbox profile or untrusted cwd = %+v", runner.request)
	}
	if approval.request.ToolName != "sandbox_exec" || approval.request.Resource.URI != "sandbox-command://sandbox-binding/python build.py" || len(approval.request.SuggestedScopes) != 1 || approval.request.SuggestedScopes[0] != approvalmodel.GrantScopeOnce {
		t.Fatalf("approval = %+v", approval.request)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["environment"] != "sandbox" || payload["cwd"] != workspace.WorkDir {
		t.Fatalf("result = %s", out)
	}
}

func TestSandboxExecRejectsUntrustedProviderAndMissingRunContext(t *testing.T) {
	_, err := New(Deps{Runner: &execToolRunner{}, InputResolver: &execToolResolver{}, InputStager: &execToolStager{}, Approval: &execToolApproval{}, Sandbox: execmodel.SandboxProfile{Provider: "other"}})
	if err == nil {
		t.Fatal("untrusted provider should be rejected")
	}
	tool, err := New(Deps{Runner: &execToolRunner{}, InputResolver: &execToolResolver{}, InputStager: &execToolStager{}, Approval: &execToolApproval{}, Sandbox: execmodel.SandboxProfile{Provider: "genesis-sandbox"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), `{"command":"echo hi"}`); err == nil {
		t.Fatal("missing PreparedRun should fail closed")
	}
}
