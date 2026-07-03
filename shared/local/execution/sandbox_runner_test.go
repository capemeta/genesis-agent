package execution

import (
	"context"
	"errors"
	"testing"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	localsandbox "genesis-agent/shared/local/sandbox"
)

type fakeArgvRunner struct {
	cmd         ArgvCommand
	constrained bool
}

func (r *fakeArgvRunner) RunArgv(ctx context.Context, command ArgvCommand, opts execcontract.RunOptions) (*execmodel.Result, error) {
	r.cmd = command
	return &execmodel.Result{Command: command.DisplayCommand, Cwd: command.Cwd, Shell: command.Shell, ExitCode: 0}, nil
}

func (r *fakeArgvRunner) RunArgvProcessConstrained(ctx context.Context, command ArgvCommand, opts execcontract.RunOptions) (*execmodel.Result, error) {
	r.cmd = command
	r.constrained = true
	return &execmodel.Result{Command: command.DisplayCommand, Cwd: command.Cwd, Shell: command.Shell, ExitCode: 0}, nil
}

type fakeSandboxBackend struct {
	plan *localsandbox.Plan
	err  error
}

func (b fakeSandboxBackend) Detect(ctx context.Context) ([]localsandbox.Capability, error) {
	return []localsandbox.Capability{{Type: localsandbox.TypeLinuxBubblewrap, Available: b.err == nil, Enforcement: localsandbox.EnforcementFilesystemNetwork}}, nil
}

func (b fakeSandboxBackend) BuildPlan(ctx context.Context, req localsandbox.BuildRequest) (*localsandbox.Plan, error) {
	if b.err != nil {
		return nil, b.err
	}
	if b.plan != nil {
		return b.plan, nil
	}
	return &localsandbox.Plan{Type: localsandbox.TypeLinuxBubblewrap, Enforcement: localsandbox.EnforcementFilesystemNetwork, Command: req.Command.Clone(), FileSystemPolicy: req.Profile.FileSystem, NetworkPolicy: req.Profile.Network, ProcessPolicy: req.Profile.Process, EffectiveSandboxProfile: req.Profile}, nil
}

func TestSandboxRunnerBuildsPlanAndRunsArgv(t *testing.T) {
	cwd := absPathForTest(t)
	argvRunner := &fakeArgvRunner{}
	manager := localsandbox.NewManagerWithBackend(fakeSandboxBackend{plan: &localsandbox.Plan{Type: localsandbox.TypeLinuxBubblewrap, Enforcement: localsandbox.EnforcementFilesystemNetwork, Command: localsandbox.CommandSpec{Argv: []string{"bwrap", "--", "bash", "-lc", "echo hi"}, Cwd: cwd, Env: map[string]string{"A": "B"}}}})
	runner := newSandboxRunner(argvRunner, SandboxRunnerOptions{Manager: manager})
	result, err := runner.RunInSandbox(context.Background(), execmodel.Command{Command: "echo hi", Cwd: cwd, Env: map[string]string{"A": "B"}, Shell: execmodel.ShellBash}, execmodel.SandboxProfile{Mode: execmodel.SandboxRequired}, execcontract.RunOptions{})
	if err != nil {
		t.Fatalf("RunInSandbox() error = %v", err)
	}
	if argvRunner.cmd.Argv[0] != "bwrap" || argvRunner.cmd.Argv[len(argvRunner.cmd.Argv)-1] != "echo hi" {
		t.Fatalf("argv runner command = %#v", argvRunner.cmd.Argv)
	}
	if result.Environment != execmodel.EnvironmentSandbox || result.SandboxProvider != string(localsandbox.TypeLinuxBubblewrap) {
		t.Fatalf("result sandbox metadata = %+v", result)
	}
}

func TestSandboxRunnerRequiredFailsClosed(t *testing.T) {
	manager := localsandbox.NewManagerWithBackend(fakeSandboxBackend{err: localsandbox.NewError(localsandbox.ErrCodeSandboxUnavailable, errors.New("no sandbox"))})
	runner := newSandboxRunner(&fakeArgvRunner{}, SandboxRunnerOptions{Manager: manager})
	_, err := runner.RunInSandbox(context.Background(), execmodel.Command{Command: "echo hi", Cwd: absPathForTest(t)}, execmodel.SandboxProfile{Mode: execmodel.SandboxRequired}, execcontract.RunOptions{})
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeSandboxUnavailable {
		t.Fatalf("CodeOf(err) = %s, want %s (%v)", code, execcontract.ErrCodeSandboxUnavailable, err)
	}
}

func TestSandboxRunnerOptionalFallbackAddsWarning(t *testing.T) {
	manager := localsandbox.NewManagerWithBackend(fakeSandboxBackend{err: localsandbox.NewError(localsandbox.ErrCodeSandboxUnavailable, errors.New("no sandbox"))})
	argvRunner := &fakeArgvRunner{}
	runner := newSandboxRunner(argvRunner, SandboxRunnerOptions{Manager: manager})
	result, err := runner.RunInSandbox(context.Background(), execmodel.Command{Command: "echo hi", Cwd: absPathForTest(t)}, execmodel.SandboxProfile{Mode: execmodel.SandboxOptional}, execcontract.RunOptions{})
	if err != nil {
		t.Fatalf("RunInSandbox() error = %v", err)
	}
	if result.Environment != execmodel.EnvironmentLocal || result.SandboxProvider != "" {
		t.Fatalf("expected local fallback metadata, got %+v", result)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected fallback warning, got %+v", result)
	}
	if argvRunner.cmd.Argv[len(argvRunner.cmd.Argv)-1] != "echo hi" {
		t.Fatalf("argv changed: %#v", argvRunner.cmd.Argv)
	}
}

func absPathForTest(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestSandboxRunnerUsesWindowsProcessConstrainedRunner(t *testing.T) {
	cwd := absPathForTest(t)
	argvRunner := &fakeArgvRunner{}
	manager := localsandbox.NewManagerWithBackend(fakeSandboxBackend{plan: &localsandbox.Plan{Type: localsandbox.TypeWindowsProcessConstrained, Enforcement: localsandbox.EnforcementProcessConstrained, Command: localsandbox.CommandSpec{Argv: []string{"cmd.exe", "/d", "/c", "echo hi"}, Cwd: cwd}}})
	runner := newSandboxRunner(argvRunner, SandboxRunnerOptions{Manager: manager})
	result, err := runner.RunInSandbox(context.Background(), execmodel.Command{Command: "echo hi", Cwd: cwd, Shell: execmodel.ShellCmd}, execmodel.SandboxProfile{Mode: execmodel.SandboxRequired}, execcontract.RunOptions{})
	if err != nil {
		t.Fatalf("RunInSandbox() error = %v", err)
	}
	if !argvRunner.constrained {
		t.Fatal("expected Windows process constrained runner to be used")
	}
	if result.Environment != execmodel.EnvironmentSandbox || result.SandboxProvider != string(localsandbox.TypeWindowsProcessConstrained) {
		t.Fatalf("result sandbox metadata = %+v", result)
	}
}
