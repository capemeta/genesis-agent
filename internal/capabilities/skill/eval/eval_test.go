package eval

import (
	"testing"
	"testing/fstest"
)

func TestValidateFSAcceptsAnthropicStyleEvals(t *testing.T) {
	root := fstest.MapFS{
		"evals/evals.json":      {Data: []byte(`{"skill_name":"demo-skill","evals":[{"id":1,"prompt":"Create a report","expected_output":"A report exists","files":["evals/files/input.txt"],"expectations":["The report contains a title"]}]}`)},
		"evals/files/input.txt": {Data: []byte("input")},
	}
	result := NewValidator().ValidateFS(root, "demo-skill")
	if result.HasErrors() {
		t.Fatalf("expected no errors, got %+v", result.Findings)
	}
	if !result.Found || result.Summary.EvalCount != 1 || result.Summary.FileCount != 1 || result.Summary.ExpectationCount != 1 {
		t.Fatalf("unexpected summary: %+v", result)
	}
}

func TestValidateFSRejectsMismatchAndUnsafeFiles(t *testing.T) {
	root := fstest.MapFS{
		"evals/evals.json": {Data: []byte(`{"skill_name":"other-skill","evals":[{"id":1,"prompt":"Run","files":["../secret.txt","C:/secret.txt"],"expectations":[]},{"id":1,"prompt":"Again","expectations":[]}]}`)},
	}
	result := NewValidator().ValidateFS(root, "demo-skill")
	if !result.HasErrors() {
		t.Fatalf("expected errors, got %+v", result.Findings)
	}
	assertEvalFinding(t, result, SeverityError, "eval_skill_name_mismatch")
	assertEvalFinding(t, result, SeverityError, "eval_file_path_invalid")
	assertEvalFinding(t, result, SeverityError, "eval_id_duplicate")
}

func TestValidateFSWarnsWhenEvalsDirHasNoManifest(t *testing.T) {
	root := fstest.MapFS{
		"evals/.keep": {Data: []byte("")},
	}
	result := NewValidator().ValidateFS(root, "demo-skill")
	if result.HasErrors() {
		t.Fatalf("expected no errors, got %+v", result.Findings)
	}
	assertEvalFinding(t, result, SeverityWarning, "evals_json_missing")
}

func TestValidateRunFSAcceptsGradingMetricsAndTiming(t *testing.T) {
	root := fstest.MapFS{
		"grading.json":         {Data: []byte(`{"expectations":[{"text":"The report exists","passed":true,"evidence":"outputs/report.md exists"},{"text":"The report has a title","passed":false,"evidence":"No title found"}],"summary":{"passed":1,"failed":1,"total":2,"pass_rate":0.5},"claims":[{"claim":"The report exists","type":"factual","verified":true,"evidence":"outputs/report.md exists"}]}`)},
		"outputs/metrics.json": {Data: []byte(`{"tool_calls":{"read_file":1,"write_file":1},"total_tool_calls":2,"total_steps":2,"errors_encountered":0,"output_chars":120,"transcript_chars":320}`)},
		"timing.json":          {Data: []byte(`{"total_tokens":100,"duration_ms":2000,"total_duration_seconds":2}`)},
	}
	result := NewValidator().ValidateRunFS(root)
	if result.HasErrors() {
		t.Fatalf("expected no errors, got %+v", result.Findings)
	}
	if !result.Found || result.Summary.Expectations != 2 || result.Summary.Claims != 1 {
		t.Fatalf("unexpected run summary: %+v", result)
	}
}

func TestValidateRunFSRejectsInconsistentSummaryAndNegativeMetrics(t *testing.T) {
	root := fstest.MapFS{
		"grading.json": {Data: []byte(`{"expectations":[{"text":"A","passed":true,"evidence":"ok"}],"summary":{"passed":0,"failed":1,"total":2,"pass_rate":0.5},"execution_metrics":{"total_tool_calls":-1}}`)},
	}
	result := NewValidator().ValidateRunFS(root)
	if !result.HasErrors() {
		t.Fatalf("expected errors, got %+v", result.Findings)
	}
	assertEvalRunFinding(t, result, SeverityError, "grading_summary_total_mismatch")
	assertEvalRunFinding(t, result, SeverityError, "grading_summary_counts_mismatch")
	assertEvalRunFinding(t, result, SeverityError, "grading_pass_rate_mismatch")
	assertEvalRunFinding(t, result, SeverityError, "metrics_negative_value")
}

func assertEvalRunFinding(t *testing.T, result RunValidationResult, severity Severity, code string) {
	t.Helper()
	for _, finding := range result.Findings {
		if finding.Severity == severity && finding.Code == code {
			return
		}
	}
	t.Fatalf("missing finding severity=%s code=%s in %+v", severity, code, result.Findings)
}
func assertEvalFinding(t *testing.T, result ValidationResult, severity Severity, code string) {
	t.Helper()
	for _, finding := range result.Findings {
		if finding.Severity == severity && finding.Code == code {
			return
		}
	}
	t.Fatalf("missing finding severity=%s code=%s in %+v", severity, code, result.Findings)
}
