package run_command

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/capabilities/tool/scheduler"
	"genesis-agent/internal/platform/contextutil"
)

type fakeResolver struct {
	path fsmodel.ResolvedPath
	err  error
}

func (r fakeResolver) Resolve(ctx context.Context, ref fsmodel.PathRef, opts fscontract.ResolveOptions) (fsmodel.ResolvedPath, error) {
	if r.err != nil {
		return fsmodel.ResolvedPath{}, r.err
	}
	return r.path, nil
}

type fakeApproval struct {
	decision approvalmodel.Decision
	lastReq  approvalmodel.Request
}

func (a *fakeApproval) Authorize(ctx context.Context, req approvalmodel.Request) (approvalmodel.Decision, error) {
	a.lastReq = req
	return a.decision, nil
}

type fakeRunner struct {
	called bool
	cmd    execmodel.Command
	opts   execcontract.RunOptions
}

func (r *fakeRunner) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	r.called = true
	r.cmd = cmd
	r.opts = opts
	return &execmodel.Result{Command: cmd.Command, Cwd: cmd.Cwd, Shell: cmd.Shell, ExitCode: 0, Stdout: "ok", DurationMS: 1}, nil
}

func TestToolExecuteRunsApprovedCommand(t *testing.T) {
	approval := &fakeApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionApproved}}
	runner := &fakeRunner{}
	tool := newTestTool(t, approval, runner, execmodel.SandboxProfile{Mode: execmodel.SandboxOptional, Provider: "genesis-sandbox"})
	out, err := tool.Execute(context.Background(), `{"command":"echo hi","shell":"bash","timeout_ms":1000,"max_output_bytes":8}`)
	if err != nil {
		t.Fatal(err)
	}
	if !runner.called {
		t.Fatal("runner was not called")
	}
	if runner.cmd.Command != "echo hi" || runner.cmd.Cwd != "C:/workspace" || runner.cmd.Shell != execmodel.ShellBash {
		t.Fatalf("runner cmd = %+v", runner.cmd)
	}
	if runner.opts.Timeout != time.Second || runner.opts.MaxOutputBytes != 8 || runner.opts.Sandbox.Mode != execmodel.SandboxOptional {
		t.Fatalf("runner opts = %+v", runner.opts)
	}
	var result execmodel.Result
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "ok" || result.Shell != execmodel.ShellBash {
		t.Fatalf("result = %+v", result)
	}
}

func TestToolExecuteDeniedApprovalDoesNotRun(t *testing.T) {
	approval := &fakeApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionDenied, Reason: "no"}}
	runner := &fakeRunner{}
	tool := newTestTool(t, approval, runner, execmodel.SandboxProfile{})
	_, err := tool.Execute(context.Background(), `{"command":"echo hi"}`)
	if err == nil {
		t.Fatal("err = nil, want permission error")
	}
	if runner.called {
		t.Fatal("runner called after denied approval")
	}
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodePermissionDenied {
		t.Fatalf("CodeOf(err) = %s", code)
	}
}

func TestToolExecuteEnvMarksReadOnlyCommandDangerous(t *testing.T) {
	approval := &fakeApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionDenied, Reason: "needs approval"}}
	runner := &fakeRunner{}
	tool := newTestTool(t, approval, runner, execmodel.SandboxProfile{})
	_, _ = tool.Execute(context.Background(), `{"command":"echo hi","env":{"A":"B"}}`)
	if approval.lastReq.Metadata["dangerous"] != "true" {
		t.Fatalf("dangerous metadata = %q", approval.lastReq.Metadata["dangerous"])
	}
	if !strings.Contains(approval.lastReq.Reason, "environment") {
		t.Fatalf("Reason = %q, want environment", approval.lastReq.Reason)
	}
}

func TestToolExecuteUsesContextSandboxOverride(t *testing.T) {
	approval := &fakeApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionApproved}}
	runner := &fakeRunner{}
	tool := newTestTool(t, approval, runner, execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled})
	override := execmodel.SandboxProfile{
		Mode:           execmodel.SandboxRequired,
		Provider:       "genesis-sandbox",
		RuntimeProfile: execmodel.RuntimeProfileCodePythonIsolated,
		TaskType:       execmodel.SandboxTaskShell,
		Operation:      execmodel.SandboxOperationRunShell,
		Language:       "shell",
	}
	ctx := contextutil.WithSandboxProfileOverride(context.Background(), override)
	if _, err := tool.Execute(ctx, `{"command":"echo hi"}`); err != nil {
		t.Fatal(err)
	}
	if runner.opts.Sandbox.Mode != execmodel.SandboxRequired ||
		runner.opts.Sandbox.Provider != "genesis-sandbox" ||
		runner.opts.Sandbox.RuntimeProfile != execmodel.RuntimeProfileCodePythonIsolated {
		t.Fatalf("sandbox override = %+v", runner.opts.Sandbox)
	}
}

func TestToolExecuteRejectsUnknownShell(t *testing.T) {
	approval := &fakeApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionApproved}}
	runner := &fakeRunner{}
	tool := newTestTool(t, approval, runner, execmodel.SandboxProfile{})
	_, err := tool.Execute(context.Background(), `{"command":"echo hi","shell":"fish"}`)
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err) = %s, want %s", code, execcontract.ErrCodeInvalidInput)
	}
	if runner.called {
		t.Fatal("runner called after invalid shell")
	}
}

func newTestTool(t *testing.T, approval *fakeApproval, runner *fakeRunner, sandbox execmodel.SandboxProfile) *Tool {
	t.Helper()
	tool, err := New(Deps{
		Runner: runner,
		Resolver: fakeResolver{path: fsmodel.ResolvedPath{
			DisplayPath:  "C:/workspace",
			BackendPath:  "C:/workspace",
			WorkspaceID:  "test-workspace",
			WorkspaceRel: ".",
			Scope:        fsmodel.PathScopeWorkspace,
		}},
		Approval: approval,
		Locker:   scheduler.NewMemoryResourceLocker(),
		Sandbox:  sandbox,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tool.(*Tool)
}
