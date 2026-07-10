package service

import (
	"testing"

	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
)

func TestClassifyFailureFromScriptHint(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:       false,
		Skill:    "office-ppt",
		Script:   "office-ppt/scripts/create_pptx.js",
		Stdout:   `{"ok":false,"errors":["pptxgenjs not installed"],"hint":"dependency_missing","dependency":"pptxgenjs"}`,
		ExitCode: 1,
	}
	classifyFailure(out)
	if out.FailureKind != "dependency_missing" {
		t.Fatalf("FailureKind=%q", out.FailureKind)
	}
	if len(out.Missing) != 1 || out.Missing[0].Name != "pptxgenjs" || out.Missing[0].Manager != "npm" {
		t.Fatalf("Missing=%+v", out.Missing)
	}
	if out.SuggestedAction != "install_then_retry" || !out.Retryable {
		t.Fatalf("SuggestedAction=%q retryable=%v", out.SuggestedAction, out.Retryable)
	}
	if out.SuggestedInstall == nil || out.SuggestedInstall.Tool != "install_skill_dependencies" {
		t.Fatalf("SuggestedInstall=%+v", out.SuggestedInstall)
	}
	if out.SuggestedInstall.ShellFallback != "npm install pptxgenjs" {
		t.Fatalf("ShellFallback=%q", out.SuggestedInstall.ShellFallback)
	}
}

func TestClassifyFailureFromPythonStderr(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:       false,
		Skill:    "office-ppt",
		Script:   "office-ppt/scripts/thumbnail.py",
		Stderr:   `ModuleNotFoundError: No module named 'PIL'`,
		ExitCode: 1,
	}
	classifyFailure(out)
	if out.FailureKind != "dependency_missing" {
		t.Fatalf("FailureKind=%q", out.FailureKind)
	}
	if len(out.Missing) != 1 || out.Missing[0].Name != "PIL" || out.Missing[0].Manager != "pip" {
		t.Fatalf("Missing=%+v", out.Missing)
	}
}

func TestClassifyFailureFromNodeStderr(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:       false,
		Script:   "x.js",
		Stderr:   `Error: Cannot find module 'pptxgenjs'`,
		ExitCode: 1,
	}
	classifyFailure(out)
	if out.FailureKind != "dependency_missing" || len(out.Missing) != 1 || out.Missing[0].Name != "pptxgenjs" {
		t.Fatalf("got kind=%q missing=%+v", out.FailureKind, out.Missing)
	}
}

func TestClassifyFailureSkipsWhenOK(t *testing.T) {
	out := &scriptcontract.RunResult{OK: true, Stdout: `{"ok":true}`}
	classifyFailure(out)
	if out.FailureKind != "" {
		t.Fatalf("unexpected kind %q", out.FailureKind)
	}
}

func TestClassifyFailureRunnerErrorWithoutExitCode(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:    false,
		Error: "dial tcp: connection refused",
	}
	classifyFailure(out)
	if out.FailureKind != "script_error" {
		t.Fatalf("FailureKind=%q, want script_error for zero exit_code failures", out.FailureKind)
	}
}

func TestClassifyFailureApprovalDenied(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:    false,
		Error: "approval denied: user rejected",
	}
	classifyFailure(out)
	if out.FailureKind != "approval_denied" {
		t.Fatalf("FailureKind=%q", out.FailureKind)
	}
}

func TestClassifyFailureTimeout(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:    false,
		Error: "context deadline exceeded: timeout",
	}
	classifyFailure(out)
	if out.FailureKind != "timeout" {
		t.Fatalf("FailureKind=%q", out.FailureKind)
	}
}

func TestClassifyFailureSandboxViolationPreserved(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:          false,
		FailureKind: "sandbox_violation",
		Error:       "sandbox violation",
	}
	classifyFailure(out)
	if out.FailureKind != "sandbox_violation" || out.SuggestedAction != "escalate_or_change_sandbox" || !out.Retryable {
		t.Fatalf("kind=%q action=%q retryable=%v", out.FailureKind, out.SuggestedAction, out.Retryable)
	}
}

func TestClassifyFailureSystemOnly(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:          false,
		Skill:       "office-ppt",
		Script:      "office-ppt/scripts/render_pptx_preview.py",
		FailureKind: "dependency_missing",
		Missing:     []scriptcontract.MissingDep{{Manager: "system", Name: "libreoffice", Require: "soffice"}},
		Error:       "preflight: dependency_missing",
	}
	classifyFailure(out)
	if out.SuggestedAction != "use_preinstalled_image_or_local_toolchain" {
		t.Fatalf("SuggestedAction=%q", out.SuggestedAction)
	}
	if out.Retryable {
		t.Fatal("system-only missing must not be retryable via install")
	}
	if out.SuggestedInstall != nil && out.SuggestedInstall.Tool != "" {
		t.Fatalf("must not suggest install tool for system-only: %+v", out.SuggestedInstall)
	}
}
