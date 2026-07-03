package policy

import (
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
)

func TestClassifyReadOnlyCommand(t *testing.T) {
	cls := Classify("git status")
	if !cls.ReadOnly || cls.Dangerous || cls.Destructive || cls.Critical {
		t.Fatalf("Classify(git status) = %+v, want read-only", cls)
	}
}

func TestClassifyDestructiveCommand(t *testing.T) {
	cls := Classify("rm -rf build")
	if !cls.Dangerous || !cls.Destructive || cls.Critical {
		t.Fatalf("Classify(rm -rf build) = %+v, want destructive", cls)
	}
}

func TestClassifyCriticalCommand(t *testing.T) {
	cls := Classify("shutdown /s")
	if !cls.Critical || !cls.Destructive {
		t.Fatalf("Classify(shutdown /s) = %+v, want critical", cls)
	}
}

func TestClassifyCompoundCommandIsDangerous(t *testing.T) {
	cls := Classify("echo hi && npm install")
	if !cls.Dangerous || cls.ReadOnly {
		t.Fatalf("Classify(compound) = %+v, want dangerous", cls)
	}
}
func TestBuildApprovalRequestForExternalCommand(t *testing.T) {
	cmd := execmodel.Command{Command: "echo hi", Shell: execmodel.ShellBash}
	req := BuildApprovalRequest("run_command", cmd, fsmodel.ResolvedPath{
		DisplayPath: "C:/tmp",
		BackendPath: "C:/tmp",
		Scope:       fsmodel.PathScopeExternal,
	}, Classify(cmd.Command))
	if req.Action != approvalmodel.ActionCommandExec {
		t.Fatalf("Action = %s, want %s", req.Action, approvalmodel.ActionCommandExec)
	}
	if req.Metadata["scope"] != string(fsmodel.PathScopeExternal) {
		t.Fatalf("scope metadata = %q", req.Metadata["scope"])
	}
	if req.Metadata["shell"] != string(execmodel.ShellBash) {
		t.Fatalf("shell metadata = %q", req.Metadata["shell"])
	}
	if req.Risk != approvalmodel.RiskHigh {
		t.Fatalf("Risk = %s, want high", req.Risk)
	}
	if len(req.SuggestedScopes) != 1 || req.SuggestedScopes[0] != approvalmodel.GrantScopeOnce {
		t.Fatalf("SuggestedScopes = %+v, want once", req.SuggestedScopes)
	}
}
