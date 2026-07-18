package service

import (
	"context"
	"testing"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

type fakeDirectRunner struct {
	called bool
	cmd    execmodel.Command
}

func (r *fakeDirectRunner) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	r.called = true
	r.cmd = cmd
	return &execmodel.Result{Command: cmd.Command, Environment: execmodel.EnvironmentLocal}, nil
}

type fakeSandboxRunner struct {
	called bool
	result *execmodel.Result
	err    error
}

func (r *fakeSandboxRunner) RunInSandbox(ctx context.Context, cmd execmodel.Command, sandbox execmodel.SandboxProfile, opts execcontract.RunOptions) (*execmodel.Result, error) {
	r.called = true
	if r.err != nil {
		return nil, r.err
	}
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
	result, err := runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, projectRunOptions())
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

func TestRunnerInjectsLocalWorkspaceEnvWithoutChangingCwd(t *testing.T) {
	direct := &fakeDirectRunner{}
	runner, err := NewRunner(direct, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), execmodel.Command{
		Command: "go test ./...",
		Cwd:     `D:\workspace\go\genesis-agent`,
		Env:     map[string]string{"A": "B"},
	}, projectRunOptions())
	if err != nil {
		t.Fatal(err)
	}
	if direct.cmd.Cwd != `D:\workspace\go\genesis-agent` {
		t.Fatalf("cwd changed: %q", direct.cmd.Cwd)
	}
	if direct.cmd.Env["WORK_DIR"] != `D:\workspace\go\genesis-agent` || direct.cmd.Env["GENESIS_WORKSPACE"] != `D:\workspace\go\genesis-agent` || direct.cmd.Env["TMPDIR"] == "" || direct.cmd.Env["A"] != "B" {
		t.Fatalf("env=%+v", direct.cmd.Env)
	}
	if _, ok := direct.cmd.Env["OUTPUT_DIR"]; ok {
		t.Fatalf("local coding mode should not invent OUTPUT_DIR: %+v", direct.cmd.Env)
	}
}

func TestRunnerUsesSandboxWhenRequired(t *testing.T) {
	direct := &fakeDirectRunner{}
	sandbox := &fakeSandboxRunner{}
	runner, err := NewRunner(direct, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	opts := projectRunOptions()
	opts.Sandbox = execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"}
	result, err := runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, opts)
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

func TestRunnerStrictRemoteRejectsHostAbsolutePath(t *testing.T) {
	direct := &fakeDirectRunner{}
	sandbox := &fakeSandboxRunner{}
	runner, err := NewRunner(direct, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	opts := taskRunOptions()
	opts.Sandbox = execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"}
	_, err = runner.Run(context.Background(), execmodel.Command{Command: `python read.py D:\data\input.xlsx`}, opts)
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if direct.called || sandbox.called {
		t.Fatalf("runner should not execute after preflight failure: direct=%t sandbox=%t", direct.called, sandbox.called)
	}
}

func TestRunnerUsesInjectedPathValidator(t *testing.T) {
	direct := &fakeDirectRunner{}
	validator := &fakePathValidator{err: execcontract.NewError(execcontract.ErrCodeInvalidInput, nil)}
	runner, err := NewRunner(direct, nil, WithPathValidator(validator))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, projectRunOptions())
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !validator.called {
		t.Fatal("injected validator was not called")
	}
	if direct.called {
		t.Fatal("direct runner should not execute after injected validator failure")
	}
}

func TestRunnerPermissionOnlyAllowsLocalCodingAbsolutePath(t *testing.T) {
	direct := &fakeDirectRunner{}
	runner, err := NewRunner(direct, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), execmodel.Command{Command: `type D:\workspace\go\genesis-agent\go.mod`}, projectRunOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !direct.called {
		t.Fatal("direct runner was not called")
	}
}

func TestRunnerOptionalSandboxMissingReturnsToHarness(t *testing.T) {
	direct := &fakeDirectRunner{}
	runner, err := NewRunner(direct, nil)
	if err != nil {
		t.Fatal(err)
	}
	opts := projectRunOptions()
	opts.Sandbox = execmodel.SandboxProfile{Mode: execmodel.SandboxOptional}
	_, err = runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, opts)
	if execcontract.CodeOf(err) != execcontract.ErrCodeSandboxUnavailable {
		t.Fatalf("expected sandbox unavailable, got %v", err)
	}
	if direct.called {
		t.Fatal("runner 不得在同一 binding 内静默切换 backend")
	}
}

func TestRunnerOptionalSandboxUnavailableReturnsToHarness(t *testing.T) {
	direct := &fakeDirectRunner{}
	sandbox := &fakeSandboxRunner{err: execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, nil)}
	runner, err := NewRunner(direct, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	opts := projectRunOptions()
	opts.Sandbox = execmodel.SandboxProfile{Mode: execmodel.SandboxOptional}
	_, err = runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, opts)
	if execcontract.CodeOf(err) != execcontract.ErrCodeSandboxUnavailable {
		t.Fatalf("expected sandbox unavailable, got %v", err)
	}
	if !sandbox.called || direct.called {
		t.Fatalf("sandbox=%t direct=%t", sandbox.called, direct.called)
	}
}

func TestRunnerOptionalSandboxPermissionDeniedDoesNotFallback(t *testing.T) {
	direct := &fakeDirectRunner{}
	sandbox := &fakeSandboxRunner{err: execcontract.NewError(execcontract.ErrCodePermissionDenied, nil)}
	runner, err := NewRunner(direct, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	opts := projectRunOptions()
	opts.Sandbox = execmodel.SandboxProfile{Mode: execmodel.SandboxOptional}
	_, err = runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, opts)
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodePermissionDenied {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if direct.called {
		t.Fatal("permission denied should not fall back to direct runner")
	}
}

func TestRunnerPreservesSandboxRunnerMetadata(t *testing.T) {
	sandbox := &fakeSandboxRunner{result: &execmodel.Result{Command: "echo hi", Environment: execmodel.EnvironmentLocal, Warnings: []string{"sandbox unavailable; running locally"}}}
	runner, err := NewRunner(&fakeDirectRunner{}, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	opts := projectRunOptions()
	opts.Sandbox = execmodel.SandboxProfile{Mode: execmodel.SandboxOptional, Provider: "local-platform"}
	result, err := runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, opts)
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
	opts := projectRunOptions()
	opts.Sandbox = execmodel.SandboxProfile{Mode: execmodel.SandboxRequired}
	_, err = runner.Run(context.Background(), execmodel.Command{Command: "echo hi"}, opts)
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeSandboxUnavailable {
		t.Fatalf("CodeOf(err) = %s, want %s", code, execcontract.ErrCodeSandboxUnavailable)
	}
}

type fakePathValidator struct {
	called bool
	err    error
}

func projectRunOptions() execcontract.RunOptions {
	return execcontract.RunOptions{
		Binding: execmodel.ExecutionBinding{
			ID: "binding-project", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite,
			PathPolicy: execmodel.PathPolicyPermissionOnly, Owner: execmodel.ExecutionOwnerRef{RunID: "run-project"},
		},
		Workspace: execmodel.ExecutionWorkspace{WorkDir: `D:\workspace\go\genesis-agent`, TmpDir: `D:\workspace\go\genesis-agent\.genesis\tmp`},
	}
}

func taskRunOptions() execcontract.RunOptions {
	return execcontract.RunOptions{
		Binding: execmodel.ExecutionBinding{
			ID: "binding-task", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite,
			PathPolicy: execmodel.PathPolicyStrictWorkspace, Owner: execmodel.ExecutionOwnerRef{RunID: "run-task"},
		},
		Workspace: execmodel.ExecutionWorkspace{
			WorkDir: `D:\workspace\work`, InputDir: `D:\workspace\input`, OutputDir: `D:\workspace\output`, TmpDir: `D:\workspace\tmp`,
		},
	}
}

func (v *fakePathValidator) ValidateCommand(cmd execmodel.Command, opts execcontract.RunOptions) error {
	v.called = true
	return v.err
}
