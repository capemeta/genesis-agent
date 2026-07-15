package command

import (
	"context"
	"testing"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	"genesis-agent/internal/capabilities/hook/model"
)

type fakeExecutionRunner struct {
	result  *execmodel.Result
	command execmodel.Command
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
	decision := runner.Run(context.Background(), model.HandlerSpec{Command: "guard"}, []byte(`{"tool_name":"x"}`))
	if decision.PermissionDecision != "ask" || decision.UpdatedInput["path"] != "safe" || string(fake.command.Stdin) != `{"tool_name":"x"}` {
		t.Fatalf("unexpected: %#v, %#v", decision, fake.command)
	}
}

func TestRunnerExitCodeTwoBlocks(t *testing.T) {
	runner, _ := NewRunner(&fakeExecutionRunner{result: &execmodel.Result{ExitCode: 2, Stderr: "blocked"}}, time.Second)
	decision := runner.Run(context.Background(), model.HandlerSpec{Command: "guard"}, nil)
	if decision.Continue || decision.Reason != "blocked" {
		t.Fatalf("unexpected: %#v", decision)
	}
}
