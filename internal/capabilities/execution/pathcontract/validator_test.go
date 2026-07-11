package pathcontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func TestValidateCommandAllowsLogicalDirsInStrictMode(t *testing.T) {
	err := ValidateCommand(execmodel.Command{
		Command: `python -c "open('${INPUT_DIR}/data.csv').read(); open('${OUTPUT_DIR}/result.csv','w').write('ok')"`,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if err != nil {
		t.Fatalf("ValidateCommand() error = %v", err)
	}
}

func TestValidateCommandRejectsHostPathsInStrictMode(t *testing.T) {
	err := ValidateCommand(execmodel.Command{
		Command: `python process.py D:\data\input.xlsx --out /Users/alice/out.pdf`,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), "D:\\data\\input.xlsx") || !strings.Contains(err.Error(), "/Users/alice/out.pdf") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandRejectsTmpOutputInStrictMode(t *testing.T) {
	err := ValidateCommand(execmodel.Command{
		Command: `python -c "open('/tmp/result.csv','w').write('bad')"`,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), "TMPDIR") || !strings.Contains(err.Error(), "OUTPUT_DIR") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandRejectsNonWorkspaceAbsolutePath(t *testing.T) {
	err := ValidateCommand(execmodel.Command{
		Command: `python -c "open('/var/logs/result.txt','w').write('bad')"`,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
}

func TestValidateCommandRejectsGenericUnixAbsolutePath(t *testing.T) {
	err := ValidateCommand(execmodel.Command{
		Command: `python -c "open('/etc/passwd').read()"`,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), "/etc/passwd") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandRejectsUNCPath(t *testing.T) {
	err := ValidateCommand(execmodel.Command{
		Command: `python process.py \\server\share\data.csv`,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), `\\server\share\data.csv`) {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandDoesNotTreatURLAsFilePath(t *testing.T) {
	err := ValidateCommand(execmodel.Command{
		Command: `python -c "print('https://example.com/report.csv')"`,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if err != nil {
		t.Fatalf("ValidateCommand() error = %v", err)
	}
}

func TestValidateCommandDoesNotTreatSedDelimiterAsFilePath(t *testing.T) {
	err := ValidateCommand(execmodel.Command{
		Command: `sed "s/foo/bar/" ${INPUT_DIR}/data.txt`,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if err != nil {
		t.Fatalf("ValidateCommand() error = %v", err)
	}
}

func TestValidateCommandRejectsAbsolutePathAfterEquals(t *testing.T) {
	err := ValidateCommand(execmodel.Command{
		Command: `python tool.py --config=/etc/app/config.yaml`,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
}

func TestValidateCommandAllowsSandboxWorkspacePath(t *testing.T) {
	err := ValidateCommand(execmodel.Command{
		Command: `python -c "open('/workspace/output/result.txt','w').write('ok')"`,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if err != nil {
		t.Fatalf("ValidateCommand() error = %v", err)
	}
}

func TestValidateCommandPermissionOnlyAllowsLocalCodingPaths(t *testing.T) {
	err := ValidateCommand(execmodel.Command{
		Command: `type D:\workspace\go\genesis-agent\go.mod`,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{
			Mode:       execmodel.WorkspaceModeLocalCoding,
			PathPolicy: execmodel.PathPolicyPermissionOnly,
		},
	})
	if err != nil {
		t.Fatalf("ValidateCommand() error = %v", err)
	}
}

func TestEffectivePathPolicyRemoteDefaultsStrict(t *testing.T) {
	got := EffectivePathPolicy(execcontract.RunOptions{
		Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxRequired, Provider: "genesis-sandbox"},
	})
	if got != execmodel.PathPolicyStrictWorkspace {
		t.Fatalf("EffectivePathPolicy()=%s", got)
	}
}

func TestValidateCommandRejectsPathInsidePythonScriptFile(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "process.py")
	if err := os.WriteFile(script, []byte(`from pathlib import Path
Path("/var/logs/result.txt").write_text("bad")
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := ValidateCommand(execmodel.Command{
		Command: `python process.py`,
		Cwd:     dir,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), script) || !strings.Contains(err.Error(), "/var/logs/result.txt") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandAllowsPythonScriptUsingLogicalDirs(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "process.py")
	if err := os.WriteFile(script, []byte(`import os
input_path = os.path.join(os.environ["INPUT_DIR"], "data.csv")
output_path = os.path.join(os.environ["OUTPUT_DIR"], "result.csv")
open(input_path).read()
open(output_path, "w").write("ok")
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := ValidateCommand(execmodel.Command{
		Command: `python process.py`,
		Cwd:     dir,
	}, execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	})
	if err != nil {
		t.Fatalf("ValidateCommand() error = %v", err)
	}
}

func TestValidateCommandAllowsCreatePptxJSOfficeSkill(t *testing.T) {
	src := filepath.Join("..", "..", "skill", "adapter", "embedded", "skills", "office-ppt", "scripts", "create_pptx.js")
	data, err := os.ReadFile(src)
	if err != nil {
		alt := filepath.FromSlash("d:/workspace/go/genesis-agent/internal/capabilities/skill/adapter/embedded/skills/office-ppt/scripts/create_pptx.js")
		data, err = os.ReadFile(alt)
		if err != nil {
			t.Skip("create_pptx.js not found")
		}
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "create_pptx.js")
	if err := os.WriteFile(script, data, 0o600); err != nil {
		t.Fatal(err)
	}
	err = ValidateCommand(execmodel.Command{Command: `node create_pptx.js Ultra5.pptx Title`, Cwd: dir}, strictOptions())
	if err != nil {
		t.Fatalf("create_pptx.js must pass pathcontract, got %v", err)
	}
}

func TestValidateCommandIgnoresNaturalLanguageSlash(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "msg.js")
	if err := os.WriteFile(script, []byte(`console.log("office-basic / Node env");`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := ValidateCommand(execmodel.Command{Command: `node msg.js`, Cwd: dir}, strictOptions())
	if err != nil {
		t.Fatalf("natural-language slash must not fail: %v", err)
	}
}

func TestValidateCommandAllowsOOXMLPackagePathsInPython(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "edit.py")
	if err := os.WriteFile(script, []byte("target = '/ppt/slides/{dest}'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := ValidateCommand(execmodel.Command{Command: `python edit.py`, Cwd: dir}, strictOptions())
	if err != nil {
		t.Fatalf("OOXML package path must be allowed: %v", err)
	}
}

func TestValidateCommandRejectsPathInsideJavaScriptSource(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "process.js")
	if err := os.WriteFile(script, []byte(`const fs = require("fs");
fs.writeFileSync("/tmp/result.txt", "bad");
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := ValidateCommand(execmodel.Command{Command: `node process.js`, Cwd: dir}, strictOptions())
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), script) || !strings.Contains(err.Error(), "/tmp/result.txt") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandRejectsPathInsideTypeScriptSource(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "process.ts")
	if err := os.WriteFile(script, []byte("const path = `/var/logs/out.txt`;"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := ValidateCommand(execmodel.Command{Command: `tsx process.ts`, Cwd: dir}, strictOptions())
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), "/var/logs/out.txt") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandRejectsPathInsideGoRunSource(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "main.go")
	if err := os.WriteFile(script, []byte("package main\nfunc main(){ println(`/etc/passwd`) }\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := ValidateCommand(execmodel.Command{Command: `go run main.go`, Cwd: dir}, strictOptions())
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), "/etc/passwd") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandRejectsPathInsideGoRunPackageSource(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "main.go")
	if err := os.WriteFile(script, []byte("package main\nfunc main(){ println(\"/var/logs/out.txt\") }\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := ValidateCommand(execmodel.Command{Command: `go run .`, Cwd: dir}, strictOptions())
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), "/var/logs/out.txt") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandRejectsPathInsideJavaSource(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "Main.java")
	if err := os.WriteFile(script, []byte(`class Main { String p = "D:\data\input.xlsx"; }`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := ValidateCommand(execmodel.Command{Command: `javac Main.java`, Cwd: dir}, strictOptions())
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), `D:\data\input.xlsx`) {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandRejectsPathInsidePowerShellSource(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "run.ps1")
	if err := os.WriteFile(script, []byte(`Get-Content C:\data\input.csv`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := ValidateCommand(execmodel.Command{Command: `pwsh -File run.ps1`, Cwd: dir}, strictOptions())
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), `C:\data\input.csv`) {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandRejectsPathInsideShellScriptSource(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte(`#!/usr/bin/env bash
cat /home/alice/input.txt
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := ValidateCommand(execmodel.Command{Command: `bash run.sh`, Cwd: dir}, strictOptions())
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), "/home/alice/input.txt") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateCommandRejectsPathInsideSkillManifest(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skill, []byte("---\nname: demo\ndescription: Demo\n---\nRun `python /var/scripts/run.py`.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := ValidateCommand(execmodel.Command{Command: `genesis-cli skill validate SKILL.md`, Cwd: dir}, strictOptions())
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err)=%s err=%v", code, err)
	}
	if !strings.Contains(err.Error(), "/var/scripts/run.py") {
		t.Fatalf("err=%v", err)
	}
}

func TestCustomRegistryAnalyzerCanExtendPreflight(t *testing.T) {
	registry := NewRegistry(staticAnalyzer{violations: []Violation{{
		Fragment: "custom://bad",
		Reason:   "自定义分析器发现违规",
		Fix:      "按自定义规则修复",
	}}})
	violations, err := registry.Analyze(AnalysisInput{Command: execmodel.Command{Command: "noop"}})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(violations) != 1 || violations[0].Analyzer != "static" || violations[0].Severity != SeverityError {
		t.Fatalf("violations=%+v", violations)
	}
}

func TestEmptyRegistryUsesDefaultAnalyzers(t *testing.T) {
	violations, err := NewRegistry().Analyze(AnalysisInput{Command: execmodel.Command{
		Command: `python -c "open('/etc/passwd').read()"`,
	}})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("empty registry should keep default analyzers enabled")
	}
}

type staticAnalyzer struct {
	violations []Violation
}

func (staticAnalyzer) Name() string { return "static" }

func (a staticAnalyzer) Analyze(AnalysisInput) ([]Violation, error) {
	return a.violations, nil
}

func strictOptions() execcontract.RunOptions {
	return execcontract.RunOptions{
		Workspace: execmodel.ExecutionWorkspace{PathPolicy: execmodel.PathPolicyStrictWorkspace},
	}
}
