package command

import (
	"context"
	"testing"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/capabilities/hook/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type fakeExecutionRunner struct {
	result  *execmodel.Result
	command execmodel.Command
}

type hookTestControl struct {
	execution workmodel.PreparedExecutionSnapshot
}

func (h hookTestControl) PrepareRun(context.Context, workcontract.PrepareRunRequest) (workmodel.PreparedRun, error) {
	return workmodel.PreparedRun{}, nil
}
func (h hookTestControl) PrepareExecution(context.Context, workcontract.PrepareExecutionRequest) (workmodel.PreparedExecutionSnapshot, error) {
	return h.execution, nil
}
func (h hookTestControl) GetRunManifest(context.Context, string, string) (workmodel.RunManifest, error) {
	return workmodel.RunManifest{}, nil
}

func hookTestContext() context.Context {
	binding := execmodel.ExecutionBinding{ID: "hook-binding", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyPermissionOnly, Owner: execmodel.ExecutionOwnerRef{RunID: "run-hook"}}
	execution := workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: `D:\workspace\project`}}
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: workmodel.RunManifest{RunID: "run-hook"}, Execution: execution})
	return workcontract.WithControlPlane(ctx, hookTestControl{execution: execution})
}

func (r *fakeExecutionRunner) Run(_ context.Context, command execmodel.Command, _ execcontract.RunOptions) (*execmodel.Result, error) {
	r.command = command
	return r.result, nil
}

func TestRunnerParsesStructuredDecisionAndPassesStdin(t *testing.T) {
	fake := &fakeExecutionRunner{result: &execmodel.Result{ExitCode: 0, Stdout: `{"continue":true,"hookSpecificOutput":{"permissionDecision":"ask","updatedInput":{"path":"safe"}}}`}}
	runner, err := NewRunner(fake, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	decision := runner.Run(hookTestContext(), model.HandlerSpec{Name: "guard", Command: "guard"}, []byte(`{"tool_name":"x"}`))
	if decision.PermissionDecision != "ask" || decision.UpdatedInput["path"] != "safe" || string(fake.command.Stdin) != `{"tool_name":"x"}` {
		t.Fatalf("unexpected: %#v, %#v", decision, fake.command)
	}
}

func TestRunnerExitCodeTwoBlocks(t *testing.T) {
	runner, _ := NewRunner(&fakeExecutionRunner{result: &execmodel.Result{ExitCode: 2, Stderr: "blocked"}}, time.Second)
	decision := runner.Run(hookTestContext(), model.HandlerSpec{Name: "guard", Command: "guard"}, nil)
	if decision.Continue || decision.Reason != "blocked" {
		t.Fatalf("unexpected: %#v", decision)
	}
}
