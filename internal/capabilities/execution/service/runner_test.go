package service

import (
	"context"
	"testing"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

type fakeDirectRunner struct{ called bool }

func (r *fakeDirectRunner) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	r.called = true
	return &execmodel.Result{Command: cmd.Command, Environment: execmodel.EnvironmentLocal}, nil
}

type fakeSandboxRunner struct {
	called bool
	result *execmodel.Result
}

func (r *fakeSandboxRunner) RunInSandbox(ctx context.Context, cmd execmodel.Command, sandbox execmodel.SandboxProfile, opts execcontract.RunOptions) (*execmodel.Result, error) {
	r.called = true
	if r.result != nil {
		return r.result, nil
	}
	return &execmodel.Result{Command: cmd.Command, Environment: execmodel.EnvironmentSandbox, SandboxProvider: sandbox.Provider}, nil
}

func TestRunnerUsesDirectWhenSandboxDisabled(t *testing.T) {
	direct := &fakeDirectRunner{}
	sandbox := &fakeSandboxRunner{}
	runner, err := NewRunner(direct, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, execcontract.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !direct.called || sandbox.called {
		t.Fatalf("direct=%t sandbox=%t", direct.called, sandbox.called)
	}
	if result.Environment != execmodel.EnvironmentLocal {
		t.Fatalf("Environment = %s", result.Environment)
	}
}

func TestRunnerUsesSandboxWhenRequired(t *testing.T) {
	direct := &fakeDirectRunner{}
	sandbox := &fakeSandboxRunner{}
	runner, err := NewRunner(direct, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, execcontract.RunOptions{Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"}})
	if err != nil {
		t.Fatal(err)
	}
	if direct.called || !sandbox.called {
		t.Fatalf("direct=%t sandbox=%t", direct.called, sandbox.called)
	}
	if result.Environment != execmodel.EnvironmentSandbox || result.SandboxProvider != "genesis-sandbox" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunnerOptionalSandboxFallbackAddsWarning(t *testing.T) {
	direct := &fakeDirectRunner{}
	runner, err := NewRunner(direct, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, execcontract.RunOptions{Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxOptional}})
	if err != nil {
		t.Fatal(err)
	}
	if !direct.called {
		t.Fatal("direct runner was not called")
	}
	if result.Environment != execmodel.EnvironmentLocal || result.SandboxProvider != "" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != sandboxFallbackWarning {
		t.Fatalf("Warnings = %+v", result.Warnings)
	}
}

func TestRunnerPreservesSandboxRunnerMetadata(t *testing.T) {
	sandbox := &fakeSandboxRunner{result: &execmodel.Result{Command: "echo hi", Environment: execmodel.EnvironmentLocal, Warnings: []string{"sandbox unavailable; running locally"}}}
	runner, err := NewRunner(&fakeDirectRunner{}, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, execcontract.RunOptions{Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxOptional, Provider: "local-platform"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Environment != execmodel.EnvironmentLocal || result.SandboxProvider != "" {
		t.Fatalf("sandbox runner metadata was overwritten: %+v", result)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings = %+v", result.Warnings)
	}
}

func TestRunnerFailsWhenSandboxRequiredButMissing(t *testing.T) {
	runner, err := NewRunner(&fakeDirectRunner{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, execcontract.RunOptions{Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxRequired}})
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeSandboxUnavailable {
		t.Fatalf("CodeOf(err) = %s, want %s", code, execcontract.ErrCodeSandboxUnavailable)
	}
}
