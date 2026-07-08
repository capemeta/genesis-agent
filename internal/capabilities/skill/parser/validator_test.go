package parser

import (
	"testing"
	"testing/fstest"

	"genesis-agent/internal/capabilities/skill/contract"
)

func TestValidateSkillFSAcceptsMinimalSkill(t *testing.T) {
	root := fstest.MapFS{
		"SKILL.md": {Data: []byte("---\nname: code-review\ndescription: Use this skill when reviewing code changes.\n---\n# Code Review\n\nFollow the workflow.")},
	}

	result := NewValidator().ValidateSkillFS(root, contract.ParseSource{DirectoryName: "code-review"})
	if result.HasErrors() {
		t.Fatalf("expected no errors, got %+v", result.Findings)
	}
	if result.Metadata.Name != "code-review" {
		t.Fatalf("metadata name = %q", result.Metadata.Name)
	}
}

func TestValidateSkillFSReportsResourceAndScriptWarnings(t *testing.T) {
	root := fstest.MapFS{
		"SKILL.md":             {Data: []byte("---\nname: report-builder\ndescription: Use this skill when building reports.\n---\nRead `references/missing.md` and use `scripts/build.ps1`.")},
		"scripts/build.ps1":    {Data: []byte("Write-Output 'ok'")},
		"references/empty.txt": {Data: []byte("")},
	}

	result := NewValidator().ValidateSkillFS(root, contract.ParseSource{DirectoryName: "report-builder"})
	if result.HasErrors() {
		t.Fatalf("expected no errors, got %+v", result.Findings)
	}
	assertFinding(t, result, SeverityWarning, "resource_missing", "references/missing.md")
	assertFinding(t, result, SeverityWarning, "script_review_required", "scripts/build.ps1")
}

func TestValidateSkillFSIgnoresTemplateResourceReferencesInCodeFence(t *testing.T) {
	root := fstest.MapFS{
		"SKILL.md": {Data: []byte("---\nname: template-skill\ndescription: Use this skill when testing templates.\n---\n```md\n- Read `references/example.md` when needed.\n- Use `scripts/example.ps1` when needed.\n```")},
	}

	result := NewValidator().ValidateSkillFS(root, contract.ParseSource{DirectoryName: "template-skill"})
	if result.HasErrors() {
		t.Fatalf("expected no errors, got %+v", result.Findings)
	}
	for _, finding := range result.Findings {
		if finding.Code == "resource_missing" {
			t.Fatalf("did not expect resource_missing for template references: %+v", result.Findings)
		}
	}
}
func TestValidateSkillFSRejectsInvalidDescription(t *testing.T) {
	root := fstest.MapFS{
		"SKILL.md": {Data: []byte("---\nname: bad-skill\ndescription: Use <bad> skill.\n---\nBody")},
	}

	result := NewValidator().ValidateSkillFS(root, contract.ParseSource{DirectoryName: "bad-skill"})
	if !result.HasErrors() {
		t.Fatalf("expected errors, got %+v", result.Findings)
	}
	assertFinding(t, result, SeverityError, "description_angle_brackets", "SKILL.md")
}

func TestValidateSkillFSBlocksPrivateKey(t *testing.T) {
	root := fstest.MapFS{
		"SKILL.md": {Data: []byte("---\nname: key-skill\ndescription: Use this skill when testing key detection.\n---\n-----BEGIN PRIVATE KEY-----")},
	}

	result := NewValidator().ValidateSkillFS(root, contract.ParseSource{DirectoryName: "key-skill"})
	if !result.HasErrors() {
		t.Fatalf("expected private key error, got %+v", result.Findings)
	}
	assertFinding(t, result, SeverityError, "private_key_detected", "SKILL.md")
}

func TestValidateSkillFSReportsEvalErrors(t *testing.T) {
	root := fstest.MapFS{
		"SKILL.md":         {Data: []byte("---\nname: eval-skill\ndescription: Use this skill when validating evals.\n---\nBody")},
		"evals/evals.json": {Data: []byte(`{"skill_name":"other-skill","evals":[{"id":1,"prompt":"Run","files":["../secret.txt"],"expectations":[]}]}`)},
	}

	result := NewValidator().ValidateSkillFS(root, contract.ParseSource{DirectoryName: "eval-skill"})
	if !result.HasErrors() {
		t.Fatalf("expected eval errors, got %+v", result.Findings)
	}
	assertFinding(t, result, SeverityError, "eval_skill_name_mismatch", "evals/evals.json")
	assertFinding(t, result, SeverityError, "eval_file_path_invalid", "evals/evals.json")
}
func assertFinding(t *testing.T, result ValidationResult, severity Severity, code, resource string) {
	t.Helper()
	for _, finding := range result.Findings {
		if finding.Severity == severity && finding.Code == code && finding.Path == resource {
			return
		}
	}
	t.Fatalf("missing finding severity=%s code=%s path=%s in %+v", severity, code, resource, result.Findings)
}
