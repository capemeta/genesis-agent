// Package eval 定义 Skill evals/evals.json 的本地校验模型。
package eval

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"path"
	"strings"
)

const (
	EvalsPath   = "evals/evals.json"
	GradingPath = "grading.json"
	MetricsPath = "outputs/metrics.json"
	TimingPath  = "timing.json"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Finding struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Path     string   `json:"path,omitempty"`
	Message  string   `json:"message"`
}

type Suite struct {
	SkillName string         `json:"skill_name"`
	Evals     []Case         `json:"evals"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type Case struct {
	ID             int            `json:"id"`
	Name           string         `json:"name,omitempty"`
	Prompt         string         `json:"prompt"`
	ExpectedOutput string         `json:"expected_output"`
	Files          []string       `json:"files,omitempty"`
	Expectations   []string       `json:"expectations"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type Summary struct {
	EvalCount        int `json:"eval_count"`
	FileCount        int `json:"file_count"`
	ExpectationCount int `json:"expectation_count"`
}

type ValidationResult struct {
	Found    bool      `json:"found"`
	Suite    Suite     `json:"suite,omitempty"`
	Summary  Summary   `json:"summary,omitempty"`
	Findings []Finding `json:"findings,omitempty"`
}

func (r ValidationResult) HasErrors() bool {
	for _, finding := range r.Findings {
		if finding.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (r *ValidationResult) add(severity Severity, code, resource, message string) {
	r.Findings = append(r.Findings, Finding{Severity: severity, Code: code, Path: resource, Message: message})
}

type Validator struct{}

func NewValidator() *Validator { return &Validator{} }

func (v *Validator) ValidateFS(root fs.FS, expectedSkillName string) ValidationResult {
	result := ValidationResult{}
	if _, err := fs.Stat(root, "evals"); err == nil {
		if _, err := fs.Stat(root, EvalsPath); err != nil {
			result.add(SeverityWarning, "evals_json_missing", EvalsPath, "存在evals目录但缺少evals/evals.json")
			return result
		}
	}
	data, err := fs.ReadFile(root, EvalsPath)
	if err != nil {
		return result
	}
	result.Found = true
	var suite Suite
	if err := json.Unmarshal(data, &suite); err != nil {
		result.add(SeverityError, "evals_json_invalid", EvalsPath, fmt.Sprintf("evals/evals.json不是合法JSON: %v", err))
		return result
	}
	result.Suite = suite
	v.validateSuite(root, expectedSkillName, &result)
	return result
}

func (v *Validator) validateSuite(root fs.FS, expectedSkillName string, result *ValidationResult) {
	suite := result.Suite
	if strings.TrimSpace(suite.SkillName) == "" {
		result.add(SeverityError, "eval_skill_name_missing", EvalsPath, "缺少skill_name")
	} else if expectedSkillName != "" && suite.SkillName != expectedSkillName {
		result.add(SeverityError, "eval_skill_name_mismatch", EvalsPath, fmt.Sprintf("skill_name=%s 与SKILL.md name=%s不一致", suite.SkillName, expectedSkillName))
	}
	if len(suite.Evals) == 0 {
		result.add(SeverityWarning, "eval_cases_empty", EvalsPath, "evals数组为空，无法验证Skill质量")
	}
	seenIDs := map[int]struct{}{}
	for i, item := range suite.Evals {
		base := fmt.Sprintf("evals[%d]", i)
		if item.ID <= 0 {
			result.add(SeverityError, "eval_id_invalid", EvalsPath, base+".id必须是正整数")
		} else if _, ok := seenIDs[item.ID]; ok {
			result.add(SeverityError, "eval_id_duplicate", EvalsPath, fmt.Sprintf("eval id重复: %d", item.ID))
		} else {
			seenIDs[item.ID] = struct{}{}
		}
		if strings.TrimSpace(item.Prompt) == "" {
			result.add(SeverityError, "eval_prompt_missing", EvalsPath, base+".prompt不能为空")
		}
		if strings.TrimSpace(item.ExpectedOutput) == "" {
			result.add(SeverityWarning, "eval_expected_output_missing", EvalsPath, base+".expected_output为空，grader缺少成功标准")
		}
		if len(item.Expectations) == 0 {
			result.add(SeverityWarning, "eval_expectations_empty", EvalsPath, base+".expectations为空，建议写入可验证断言")
		}
		for j, expectation := range item.Expectations {
			if strings.TrimSpace(expectation) == "" {
				result.add(SeverityWarning, "eval_expectation_empty", EvalsPath, fmt.Sprintf("%s.expectations[%d]为空", base, j))
			}
		}
		for j, file := range item.Files {
			result.Summary.FileCount++
			clean, ok := normalizeEvalFile(file)
			if !ok {
				result.add(SeverityError, "eval_file_path_invalid", EvalsPath, fmt.Sprintf("%s.files[%d]必须是Skill根内的正斜杠相对路径", base, j))
				continue
			}
			if _, err := fs.Stat(root, clean); err != nil {
				result.add(SeverityWarning, "eval_file_missing", clean, "eval引用的输入文件不存在")
			}
		}
		result.Summary.ExpectationCount += len(item.Expectations)
	}
	result.Summary.EvalCount = len(suite.Evals)
}

func normalizeEvalFile(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "\\") || strings.Contains(value, ":") || strings.HasPrefix(value, "/") {
		return "", false
	}
	clean := path.Clean(strings.TrimPrefix(value, "./"))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(clean, "/../") {
		return "", false
	}
	return clean, true
}

type RunValidationResult struct {
	Found    bool       `json:"found"`
	Grading  Grading    `json:"grading,omitempty"`
	Metrics  *Metrics   `json:"metrics,omitempty"`
	Timing   *Timing    `json:"timing,omitempty"`
	Summary  RunSummary `json:"summary,omitempty"`
	Findings []Finding  `json:"findings,omitempty"`
}

type RunSummary struct {
	Expectations int `json:"expectations"`
	Claims       int `json:"claims"`
}

func (r RunValidationResult) HasErrors() bool {
	for _, finding := range r.Findings {
		if finding.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (r *RunValidationResult) add(severity Severity, code, resource, message string) {
	r.Findings = append(r.Findings, Finding{Severity: severity, Code: code, Path: resource, Message: message})
}

type Grading struct {
	Expectations     []ExpectationResult `json:"expectations"`
	Summary          GradeSummary        `json:"summary"`
	ExecutionMetrics *Metrics            `json:"execution_metrics,omitempty"`
	Timing           *Timing             `json:"timing,omitempty"`
	Claims           []Claim             `json:"claims,omitempty"`
	UserNotesSummary map[string]any      `json:"user_notes_summary,omitempty"`
	EvalFeedback     map[string]any      `json:"eval_feedback,omitempty"`
}

type ExpectationResult struct {
	Text     string `json:"text"`
	Passed   bool   `json:"passed"`
	Evidence string `json:"evidence"`
}

type GradeSummary struct {
	Passed   int     `json:"passed"`
	Failed   int     `json:"failed"`
	Total    int     `json:"total"`
	PassRate float64 `json:"pass_rate"`
}

type Metrics struct {
	ToolCalls         map[string]int `json:"tool_calls,omitempty"`
	TotalToolCalls    int            `json:"total_tool_calls,omitempty"`
	TotalSteps        int            `json:"total_steps,omitempty"`
	FilesCreated      []string       `json:"files_created,omitempty"`
	ErrorsEncountered int            `json:"errors_encountered,omitempty"`
	OutputChars       int            `json:"output_chars,omitempty"`
	TranscriptChars   int            `json:"transcript_chars,omitempty"`
}

type Timing struct {
	TotalTokens             int     `json:"total_tokens,omitempty"`
	DurationMS              int     `json:"duration_ms,omitempty"`
	TotalDurationSeconds    float64 `json:"total_duration_seconds,omitempty"`
	ExecutorStart           string  `json:"executor_start,omitempty"`
	ExecutorEnd             string  `json:"executor_end,omitempty"`
	ExecutorDurationSeconds float64 `json:"executor_duration_seconds,omitempty"`
	GraderStart             string  `json:"grader_start,omitempty"`
	GraderEnd               string  `json:"grader_end,omitempty"`
	GraderDurationSeconds   float64 `json:"grader_duration_seconds,omitempty"`
}

type Claim struct {
	Claim    string `json:"claim"`
	Type     string `json:"type"`
	Verified bool   `json:"verified"`
	Evidence string `json:"evidence"`
}

func (v *Validator) ValidateRunFS(root fs.FS) RunValidationResult {
	result := RunValidationResult{}
	data, err := fs.ReadFile(root, GradingPath)
	if err != nil {
		result.add(SeverityError, "grading_json_missing", GradingPath, "缺少grading.json")
		return result
	}
	result.Found = true
	var grading Grading
	if err := json.Unmarshal(data, &grading); err != nil {
		result.add(SeverityError, "grading_json_invalid", GradingPath, fmt.Sprintf("grading.json不是合法JSON: %v", err))
		return result
	}
	result.Grading = grading
	v.validateGrading(grading, &result)
	if data, err := fs.ReadFile(root, MetricsPath); err == nil {
		var metrics Metrics
		if err := json.Unmarshal(data, &metrics); err != nil {
			result.add(SeverityError, "metrics_json_invalid", MetricsPath, fmt.Sprintf("metrics.json不是合法JSON: %v", err))
		} else {
			result.Metrics = &metrics
			validateMetrics(metrics, MetricsPath, &result)
		}
	}
	if data, err := fs.ReadFile(root, TimingPath); err == nil {
		var timing Timing
		if err := json.Unmarshal(data, &timing); err != nil {
			result.add(SeverityError, "timing_json_invalid", TimingPath, fmt.Sprintf("timing.json不是合法JSON: %v", err))
		} else {
			result.Timing = &timing
			validateTiming(timing, TimingPath, &result)
		}
	}
	return result
}

func (v *Validator) validateGrading(grading Grading, result *RunValidationResult) {
	result.Summary.Expectations = len(grading.Expectations)
	result.Summary.Claims = len(grading.Claims)
	if len(grading.Expectations) == 0 {
		result.add(SeverityError, "grading_expectations_empty", GradingPath, "grading.expectations不能为空")
	}
	passed := 0
	failed := 0
	for i, item := range grading.Expectations {
		base := fmt.Sprintf("expectations[%d]", i)
		if strings.TrimSpace(item.Text) == "" {
			result.add(SeverityError, "grading_expectation_text_missing", GradingPath, base+".text不能为空")
		}
		if strings.TrimSpace(item.Evidence) == "" {
			result.add(SeverityWarning, "grading_evidence_missing", GradingPath, base+".evidence为空，判定缺少证据")
		}
		if item.Passed {
			passed++
		} else {
			failed++
		}
	}
	if grading.Summary.Total != len(grading.Expectations) {
		result.add(SeverityError, "grading_summary_total_mismatch", GradingPath, fmt.Sprintf("summary.total=%d 与expectations数量=%d不一致", grading.Summary.Total, len(grading.Expectations)))
	}
	if grading.Summary.Passed != passed || grading.Summary.Failed != failed {
		result.add(SeverityError, "grading_summary_counts_mismatch", GradingPath, "summary.passed/failed 与 expectations 判定数量不一致")
	}
	if grading.Summary.Total > 0 {
		expected := float64(grading.Summary.Passed) / float64(grading.Summary.Total)
		if math.Abs(grading.Summary.PassRate-expected) > 0.001 {
			result.add(SeverityError, "grading_pass_rate_mismatch", GradingPath, fmt.Sprintf("summary.pass_rate=%.4f，期望%.4f", grading.Summary.PassRate, expected))
		}
	}
	if grading.ExecutionMetrics != nil {
		validateMetrics(*grading.ExecutionMetrics, GradingPath+".execution_metrics", result)
	}
	if grading.Timing != nil {
		validateTiming(*grading.Timing, GradingPath+".timing", result)
	}
	for i, claim := range grading.Claims {
		if strings.TrimSpace(claim.Claim) == "" || strings.TrimSpace(claim.Type) == "" || strings.TrimSpace(claim.Evidence) == "" {
			result.add(SeverityWarning, "grading_claim_incomplete", GradingPath, fmt.Sprintf("claims[%d]缺少claim/type/evidence之一", i))
		}
	}
}

func validateMetrics(metrics Metrics, resource string, result interface {
	add(Severity, string, string, string)
}) {
	if metrics.TotalToolCalls < 0 || metrics.TotalSteps < 0 || metrics.ErrorsEncountered < 0 || metrics.OutputChars < 0 || metrics.TranscriptChars < 0 {
		result.add(SeverityError, "metrics_negative_value", resource, "metrics中的计数字段不能为负数")
	}
	sum := 0
	for tool, count := range metrics.ToolCalls {
		if strings.TrimSpace(tool) == "" {
			result.add(SeverityWarning, "metrics_tool_name_empty", resource, "tool_calls包含空工具名")
		}
		if count < 0 {
			result.add(SeverityError, "metrics_tool_count_negative", resource, "tool_calls中的计数不能为负数")
		}
		sum += count
	}
	if len(metrics.ToolCalls) > 0 && metrics.TotalToolCalls != sum {
		result.add(SeverityWarning, "metrics_total_tool_calls_mismatch", resource, fmt.Sprintf("total_tool_calls=%d 与tool_calls合计=%d不一致", metrics.TotalToolCalls, sum))
	}
}

func validateTiming(timing Timing, resource string, result interface {
	add(Severity, string, string, string)
}) {
	if timing.TotalTokens < 0 || timing.DurationMS < 0 || timing.TotalDurationSeconds < 0 || timing.ExecutorDurationSeconds < 0 || timing.GraderDurationSeconds < 0 {
		result.add(SeverityError, "timing_negative_value", resource, "timing中的时长和token字段不能为负数")
	}
}
