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
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
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
	result *execmodel.Result
}

type fakeShells struct{}

func (fakeShells) ShellCapabilities(context.Context) execmodel.ShellCapabilities {
	return execmodel.ShellCapabilities{
		Default: execmodel.ShellInfo{Kind: execmodel.ShellBash, Path: "bash"},
		Supported: []execmodel.ShellInfo{
			{Kind: execmodel.ShellBash, Path: "bash"},
		},
	}
}

func (r *fakeRunner) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	r.called = true
	r.cmd = cmd
	r.opts = opts
	if r.result != nil {
		result := *r.result
		result.Command = cmd.Command
		result.Cwd = cmd.Cwd
		result.Shell = cmd.Shell
		return &result, nil
	}
	return &execmodel.Result{Command: cmd.Command, Cwd: cmd.Cwd, Shell: cmd.Shell, ExitCode: 0, Stdout: "ok", DurationMS: 1}, nil
}

func TestToolInfoOnlyAdvertisesSupportedShells(t *testing.T) {
	tool := newTestTool(t, &fakeApproval{}, &fakeRunner{}, execmodel.SandboxProfile{})
	got := tool.GetInfo().Parameters.Properties["shell"].Enum
	want := []string{"auto", "bash"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("shell enum = %v, want %v", got, want)
	}
}

func TestToolExecuteRunsApprovedCommand(t *testing.T) {
	approval := &fakeApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionApproved}}
	runner := &fakeRunner{}
	tool := newTestTool(t, approval, runner, execmodel.SandboxProfile{Mode: execmodel.SandboxOptional, Provider: "genesis-sandbox"})
	out, err := tool.Execute(testRunContext(), `{"command":"echo hi","shell":"bash","timeout_ms":1000,"max_output_bytes":8}`)
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
	if runner.opts.Binding.Mode != execmodel.WorkspaceModeProject || runner.opts.Binding.PathPolicy != execmodel.PathPolicyStrictWorkspace || runner.opts.Binding.Owner.RunID != "run-command-test" || runner.opts.Workspace.WorkDir != "C:/workspace" {
		t.Fatalf("execution binding = %+v workspace = %+v", runner.opts.Binding, runner.opts.Workspace)
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
	ctx := contextutil.WithSandboxProfileOverride(testRunContext(), override)
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

func TestToolExecuteRejectsKnownButUnsupportedShell(t *testing.T) {
	approval := &fakeApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionApproved}}
	runner := &fakeRunner{}
	tool := newTestTool(t, approval, runner, execmodel.SandboxProfile{})
	_, err := tool.Execute(context.Background(), `{"command":"Get-Location","shell":"powershell"}`)
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err) = %s, want %s", code, execcontract.ErrCodeInvalidInput)
	}
	if runner.called {
		t.Fatal("runner called after unsupported shell")
	}
}

func TestToolNonZeroExitReturnsStructuredFailureAndRecoveryHint(t *testing.T) {
	approval := &fakeApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionApproved}}
	runner := &fakeRunner{result: &execmodel.Result{ExitCode: 1, Stderr: "failed"}}
	tool := newTestTool(t, approval, runner, execmodel.SandboxProfile{})
	out, err := tool.Execute(testRunContext(), `{"command":"ls D:/","shell":"bash"}`)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		OK                   bool           `json:"ok"`
		FailureKind          string         `json:"failure_kind"`
		SuggestedAction      string         `json:"suggested_action"`
		SuggestedTool        map[string]any `json:"suggested_tool"`
		OperationFingerprint string         `json:"operation_fingerprint"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || result.FailureKind != "command_exit_nonzero" || result.SuggestedAction == "" || result.SuggestedTool["name"] != "list_dir" || result.OperationFingerprint != "filesystem.list" {
		t.Fatalf("result = %+v", result)
	}
}

func TestToolExecuteRequiresRunBindingContext(t *testing.T) {
	approval := &fakeApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionApproved}}
	runner := &fakeRunner{}
	tool := newTestTool(t, approval, runner, execmodel.SandboxProfile{})
	_, err := tool.Execute(context.Background(), `{"command":"echo hi"}`)
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeExecutionBindingRequired {
		t.Fatalf("CodeOf(err) = %s, want %s, err=%v", code, execcontract.ErrCodeExecutionBindingRequired, err)
	}
	if runner.called {
		t.Fatal("runner called without trusted run context")
	}
}

func testRunContext() context.Context {
	ctx := contextutil.WithRunID(context.Background(), "run-command-test")
	ctx = contextutil.WithSessionID(ctx, "session-command-test")
	ctx = contextutil.WithTenantID(ctx, "tenant-command-test")
	binding := execmodel.ExecutionBinding{ID: "binding-command-test", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyStrictWorkspace, Owner: execmodel.ExecutionOwnerRef{RunID: "run-command-test", SessionID: "session-command-test", TenantID: "tenant-command-test"}}
	prepared := workmodel.PreparedRun{Execution: workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: "C:/workspace"}}}
	return workcontract.WithPreparedRun(ctx, prepared)
}

func newTestTool(t *testing.T, approval *fakeApproval, runner *fakeRunner, sandbox execmodel.SandboxProfile) *Tool {
	t.Helper()
	tool, err := New(Deps{
		Runner: runner,
		Shells: fakeShells{},
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
