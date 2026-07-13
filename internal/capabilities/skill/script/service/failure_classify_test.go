package service

import (
	"testing"

	skillmodel "genesis-agent/internal/capabilities/skill/model"
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

func TestClassifyFailureSystemDepFromScriptHint(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:       false,
		Skill:    "office-ppt",
		Script:   "office-ppt/scripts/render_pptx_preview.py",
		Stdout:   `{"ok":false,"errors":["libreoffice not found"],"hint":"dependency_missing","dependency":"libreoffice","manager":"system"}`,
		ExitCode: 1,
	}
	classifyFailure(out)
	if out.FailureKind != "dependency_missing" {
		t.Fatalf("FailureKind=%q", out.FailureKind)
	}
	if len(out.Missing) != 1 || out.Missing[0].Name != "libreoffice" || out.Missing[0].Manager != "system" {
		t.Fatalf("Missing=%+v", out.Missing)
	}
	if out.SuggestedAction != "use_preinstalled_image_or_local_toolchain" || out.Retryable {
		t.Fatalf("SuggestedAction=%q retryable=%v", out.SuggestedAction, out.Retryable)
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

func TestClassifyFailureNodeMissingScriptEntry(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:       false,
		Skill:    "office-ppt",
		Script:   "office-ppt/scripts/run_pptxgen_script.js",
		Stderr:   `Error: Cannot find module '/workspace/run_pptxgen_script.js'`,
		ExitCode: 1,
	}
	classifyFailure(out)
	if out.FailureKind != "script_entry_missing" {
		t.Fatalf("FailureKind=%q missing=%+v", out.FailureKind, out.Missing)
	}
	if len(out.Missing) != 0 || out.SuggestedAction != "check_skill_script_staging_or_sandbox_working_dir" || out.Retryable {
		t.Fatalf("missing=%+v action=%q retryable=%v", out.Missing, out.SuggestedAction, out.Retryable)
	}
}

func TestClassifyFailurePythonMissingScriptEntry(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:       false,
		Skill:    "office-ppt",
		Script:   "office-ppt/scripts/inspect_pptx.py",
		Stderr:   `python: can't open file '/workspace/inspect_pptx.py': [Errno 2] No such file or directory`,
		ExitCode: 2,
	}
	classifyFailure(out)
	if out.FailureKind != "script_entry_missing" {
		t.Fatalf("FailureKind=%q missing=%+v", out.FailureKind, out.Missing)
	}
}

func TestClassifyFailureSandboxInputMissing(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:       false,
		Skill:    "office-ppt",
		Script:   "office-ppt/scripts/create_pptx.js",
		Stderr:   "Traceback (most recent call last):\nFileNotFoundError: [Errno 2] No such file or directory: '/workspace/input/skill-scripts-office-ppt.zip'",
		ExitCode: 1,
	}
	classifyFailure(out)
	if out.FailureKind != "sandbox_input_missing" {
		t.Fatalf("FailureKind=%q missing=%+v", out.FailureKind, out.Missing)
	}
	if len(out.Missing) != 0 || out.SuggestedAction != "check_sandbox_input_artifact_transport" || out.Retryable {
		t.Fatalf("missing=%+v action=%q retryable=%v", out.Missing, out.SuggestedAction, out.Retryable)
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
	if out.FailureKind != "sandbox_unavailable" || out.SuggestedAction != "stop_and_report_sandbox_unavailable" || out.Retryable {
		t.Fatalf("FailureKind=%q action=%q retryable=%v", out.FailureKind, out.SuggestedAction, out.Retryable)
	}
}

func TestClassifyFailureDockerCreateErrorIsSandboxUnavailable(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:    false,
		Error: `runner_failed: 创建沙箱失败: 创建 Docker 容器失败: Error response from daemon: .sandbox\workspaces\tenant-1\ws-1 is not a valid Windows path`,
	}
	classifyFailure(out)
	if out.FailureKind != "sandbox_unavailable" {
		t.Fatalf("FailureKind=%q action=%q", out.FailureKind, out.SuggestedAction)
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
func TestClassifyFailureForSkillDoesNotSuggestInstallForUndeclaredRuntime(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:       false,
		Skill:    "office-ppt",
		Script:   "office-ppt/scripts/run_pptxgen_script.js",
		Stderr:   `Error: Cannot find module 'react'`,
		ExitCode: 1,
	}
	deps := skillmodel.Dependencies{Runtime: skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs", Require: "pptxgenjs"}}}}
	classifyFailureForSkill(out, deps)
	if out.FailureKind != "dependency_missing" {
		t.Fatalf("FailureKind=%q", out.FailureKind)
	}
	if out.SuggestedInstall != nil {
		t.Fatalf("undeclared dependency must not suggest install: %+v", out.SuggestedInstall)
	}
	if out.SuggestedAction != "rewrite_script_use_declared_dependencies" || !out.Retryable {
		t.Fatalf("action=%q retryable=%v", out.SuggestedAction, out.Retryable)
	}
}

func TestClassifyFailureForSkillAllowsDeclaredRuntimeInstall(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:       false,
		Skill:    "office-ppt",
		Script:   "office-ppt/scripts/run_pptxgen_script.js",
		Stderr:   `Error: Cannot find module 'pptxgenjs'`,
		ExitCode: 1,
	}
	deps := skillmodel.Dependencies{Runtime: skillmodel.RuntimeDeps{Node: []skillmodel.RuntimePackage{{Name: "pptxgenjs", Require: "pptxgenjs"}}}}
	classifyFailureForSkill(out, deps)
	if out.SuggestedInstall == nil || out.SuggestedInstall.Tool != "install_skill_dependencies" {
		t.Fatalf("declared dependency should keep install suggestion: %+v", out.SuggestedInstall)
	}
}

func TestClassifyFailureForSkillMatchesPythonImportAlias(t *testing.T) {
	out := &scriptcontract.RunResult{
		OK:       false,
		Skill:    "office-ppt",
		Script:   "office-ppt/scripts/thumbnail.py",
		Stderr:   `ModuleNotFoundError: No module named 'PIL'`,
		ExitCode: 1,
	}
	deps := skillmodel.Dependencies{Runtime: skillmodel.RuntimeDeps{Python: []skillmodel.RuntimePackage{{Name: "pillow", Import: "PIL"}}}}
	classifyFailureForSkill(out, deps)
	if out.SuggestedInstall == nil || out.SuggestedInstall.Tool != "install_skill_dependencies" {
		t.Fatalf("declared import alias should keep install suggestion: %+v", out.SuggestedInstall)
	}
}
