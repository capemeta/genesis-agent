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

func TestClassifyDoesNotTrustExecutableWithReadOnlyPrefix(t *testing.T) {
	cls := ClassifyCommand(execmodel.Command{Command: "ls-malicious --write", Shell: execmodel.ShellBash})
	if !cls.Dangerous || cls.ReadOnly {
		t.Fatalf("ClassifyCommand() = %+v, want dangerous", cls)
	}
}

func TestClassifyUnknownShellIsConservative(t *testing.T) {
	cls := ClassifyCommand(execmodel.Command{Command: "ls", Shell: execmodel.ShellAuto})
	if !cls.Dangerous || cls.ReadOnly {
		t.Fatalf("ClassifyCommand() = %+v, want dangerous", cls)
	}
}

func TestClassifyGitBranchMutationIsNotReadOnly(t *testing.T) {
	cls := ClassifyCommand(execmodel.Command{Command: "git branch -D obsolete", Shell: execmodel.ShellPowerShell})
	if !cls.Dangerous || cls.ReadOnly {
		t.Fatalf("ClassifyCommand() = %+v, want dangerous", cls)
	}
}

func TestClassifyPowerShellReadOnlyPipeline(t *testing.T) {
	cls := ClassifyCommand(execmodel.Command{
		Command: "Get-ChildItem -LiteralPath 'D:\\' | Select-Object -ExpandProperty Name",
		Shell:   execmodel.ShellPowerShell,
	})
	if !cls.ReadOnly || cls.Dangerous {
		t.Fatalf("ClassifyCommand() = %+v, want read-only", cls)
	}
}

func TestClassifyPowerShellMutationIsDangerous(t *testing.T) {
	cls := ClassifyCommand(execmodel.Command{Command: "Remove-Item -Recurse build", Shell: execmodel.ShellPowerShell})
	if !cls.Dangerous || !cls.Destructive {
		t.Fatalf("ClassifyCommand() = %+v, want destructive", cls)
	}
}

func TestClassifyPowerShellSingleAmpersandIsDangerous(t *testing.T) {
	cls := ClassifyCommand(execmodel.Command{Command: "Get-Location & Remove-Item build", Shell: execmodel.ShellPowerShell})
	if !cls.Dangerous || cls.ReadOnly {
		t.Fatalf("ClassifyCommand() = %+v, want dangerous", cls)
	}
}

func TestClassifyMultilineCommandIsDangerous(t *testing.T) {
	cls := ClassifyCommand(execmodel.Command{Command: "ls\nwhoami", Shell: execmodel.ShellBash})
	if !cls.Dangerous || cls.ReadOnly {
		t.Fatalf("ClassifyCommand() = %+v, want dangerous", cls)
	}
}

func TestClassifyCmdVariableExpansionIsConservative(t *testing.T) {
	cls := ClassifyCommand(execmodel.Command{Command: "echo %PATH%", Shell: execmodel.ShellCmd})
	if !cls.Dangerous || cls.ReadOnly {
		t.Fatalf("ClassifyCommand() = %+v, want dangerous", cls)
	}
}

func TestRecoveryHintForNestedPowerShellDirectoryListing(t *testing.T) {
	hint := RecoveryHint(execmodel.Command{Command: `powershell -Command "Get-ChildItem D:\\"`, Shell: execmodel.ShellCmd})
	if hint == nil || hint.Tool != "list_dir" || hint.OperationFingerprint != "filesystem.list" {
		t.Fatalf("RecoveryHint() = %+v", hint)
	}
}

func TestRecoveryHintForGlobListing(t *testing.T) {
	hint := RecoveryHint(execmodel.Command{Command: "ls -la slide-*.jpeg", Shell: execmodel.ShellBash})
	if hint == nil || hint.Tool != "glob" || hint.OperationFingerprint != "filesystem.glob" {
		t.Fatalf("RecoveryHint() = %+v", hint)
	}
}

func TestRecoveryHintIgnoreGlobOptionStaysListDir(t *testing.T) {
	hint := RecoveryHint(execmodel.Command{Command: "ls -I '*.o'", Shell: execmodel.ShellBash})
	if hint == nil || hint.Tool != "list_dir" || hint.OperationFingerprint != "filesystem.list" {
		t.Fatalf("RecoveryHint() = %+v", hint)
	}
}

func TestRecoveryHintForShellGrep(t *testing.T) {
	hint := RecoveryHint(execmodel.Command{Command: `grep -iE "lorem|ipsum" notes.md`, Shell: execmodel.ShellBash})
	if hint == nil || hint.Tool != "grep" || hint.OperationFingerprint != "filesystem.search" {
		t.Fatalf("RecoveryHint() = %+v", hint)
	}
}

func TestRecoveryHintRipgrepFilesSuggestsGlob(t *testing.T) {
	hint := RecoveryHint(execmodel.Command{Command: "rg --files -g '*.md'", Shell: execmodel.ShellBash})
	if hint == nil || hint.Tool != "glob" || hint.OperationFingerprint != "filesystem.glob" {
		t.Fatalf("RecoveryHint() = %+v", hint)
	}
}

func TestRecoveryHintRipgrepFilesWithMatchesSuggestsGrep(t *testing.T) {
	hint := RecoveryHint(execmodel.Command{Command: "rg -l lorem notes.md", Shell: execmodel.ShellBash})
	if hint == nil || hint.Tool != "grep" || hint.OperationFingerprint != "filesystem.search" {
		t.Fatalf("RecoveryHint() = %+v", hint)
	}
}

func TestRecoveryHintSkipsGrepPipeline(t *testing.T) {
	hint := RecoveryHint(execmodel.Command{Command: `python -m markitdown deck.pptx | grep -iE "lorem"`, Shell: execmodel.ShellBash})
	if hint != nil {
		t.Fatalf("pipeline grep must not suggest filesystem grep, got %+v", hint)
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
