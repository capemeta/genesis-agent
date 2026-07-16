// Package result 提供子 Run 终态的安全归约和交付投影。
package result

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/multiagent/model"
	"genesis-agent/internal/runtime/multiagent/sanitize"
)

const defaultSummaryRunes = 4_000

// TerminalCandidate 是 Reducer 唯一允许读取的子 Run 终态输入。
type TerminalCandidate struct {
	AgentID      string
	SubagentType string
	Run          *domain.Run
	Err          error
	Cancelled    bool
	Manifest     model.ArtifactManifest
	Findings     []model.Finding
}

// Reducer 将终态 Run 收敛为稳定、最小的 TaskResult。
type Reducer struct {
	MaxSummaryRunes int
	Sanitizer       sanitize.Text
	Evidence        EvidenceValidator
}

// NewReducer 创建默认安全归约器。
func NewReducer() Reducer { return Reducer{MaxSummaryRunes: defaultSummaryRunes} }

// Reduce 不读取 transcript、工具输入输出或文件系统。
func (r Reducer) Reduce(ctx context.Context, candidate TerminalCandidate) model.TaskResult {
	result := model.TaskResult{SchemaVersion: 1, AgentID: candidate.AgentID, SubagentType: candidate.SubagentType}
	if candidate.Run != nil {
		result.ChildRunID = candidate.Run.ID
		result.Usage = usageFromRun(candidate.Run)
	}

	if candidate.Cancelled || (candidate.Run != nil && candidate.Run.Status == domain.RunStatusCancelled) {
		result.Status = model.ResultStatusCancelled
		result.Error = &model.ResultError{Code: "cancelled", Message: "子智能体已取消", Retryable: true}
		result.NextAction = "可在调整任务后重新委派。"
	} else if candidate.Err != nil || (candidate.Run != nil && candidate.Run.Status == domain.RunStatusFailed) {
		result.Status = model.ResultStatusFailed
		result.Error = &model.ResultError{Code: errorCodeFor(candidate.Err), Message: r.sanitizeError(candidate.Err), Retryable: retryable(candidate.Err)}
		result.Summary, result.Truncated = r.sanitizeAndTruncate(finalAnswer(candidate.Run), r.limit())
		if result.Summary == "" {
			result.Summary = result.Error.Message
		}
		result.NextAction = "可重新委派、调整任务或稍后 resume。"
	} else {
		result.Status = model.ResultStatusCompleted
		if candidate.Run != nil && candidate.Run.Incomplete {
			result.Status = model.ResultStatusPartial
		}
		result.Summary, result.Truncated = r.sanitizeAndTruncate(finalAnswer(candidate.Run), r.limit())
		if result.Summary == "" {
			result.Status = model.ResultStatusFailed
			result.Error = &model.ResultError{Code: "empty_final_answer", Message: "子智能体未产生可安全交付的最终结论", Retryable: true}
			result.NextAction = "可重新委派或调整任务。"
		}
	}
	if result.Status == model.ResultStatusCompleted || result.Status == model.ResultStatusPartial {
		evidence, err := r.evidenceValidator().Validate(ctx, candidate.Manifest, candidate.Findings)
		if err != nil {
			result.Findings = nil
			result.Artifacts = nil
			result.OmittedSections = append(result.OmittedSections, "evidence")
			result.Status = model.ResultStatusPartial
			result.Error = &model.ResultError{Code: "evidence_validation_failed", Message: "子智能体证据未能安全验证，已省略可选结果", Retryable: true}
			result.NextAction = "可重新委派、调整任务或稍后 resume。"
		} else {
			result.Findings = cloneFindings(evidence.Findings)
			result.Artifacts = append([]model.Artifact(nil), evidence.Artifacts...)
		}
	}
	if result.Truncated {
		result.OmittedSections = append(result.OmittedSections, "summary_tail")
	}
	result.ResultID = resultID(result)
	return result
}

func (r Reducer) limit() int {
	if r.MaxSummaryRunes <= 0 {
		return defaultSummaryRunes
	}
	return r.MaxSummaryRunes
}

func usageFromRun(run *domain.Run) model.Usage {
	var usage model.Usage
	for _, step := range run.Steps {
		if step == nil {
			continue
		}
		usage.InputTokens += step.TokenUsage.PromptTokens
		usage.OutputTokens += step.TokenUsage.CompletionTokens
		if step.ActionType == domain.ActionTypeToolCall {
			usage.ToolCalls++
		}
	}
	if usage.InputTokens+usage.OutputTokens == 0 && run.TotalTokens > 0 {
		usage.OutputTokens = run.TotalTokens
	}
	return usage
}

func finalAnswer(run *domain.Run) string {
	if run == nil {
		return ""
	}
	return run.FinalAnswer
}

func (r Reducer) sanitizeError(err error) string {
	if err == nil {
		return "子智能体执行失败"
	}
	message, _ := r.sanitizeAndTruncate(err.Error(), 800)
	if message == "" {
		return "子智能体执行失败"
	}
	return message
}

func (r Reducer) sanitizeAndTruncate(value string, limit int) (string, bool) {
	value = strings.TrimSpace(value)
	cleaned, err := r.sanitizer().Sanitize(value)
	if err != nil {
		return "", false
	}
	value = cleaned
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value, false
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit])) + "...", true
}

func (r Reducer) sanitizer() sanitize.Text {
	if r.Sanitizer != nil {
		return r.Sanitizer
	}
	return sanitize.Default{}
}

func (r Reducer) evidenceValidator() EvidenceValidator {
	if r.Evidence != nil {
		return r.Evidence
	}
	return RejectingEvidenceValidator{}
}

func cloneFindings(findings []model.Finding) []model.Finding {
	if len(findings) == 0 {
		return nil
	}
	out := make([]model.Finding, 0, len(findings))
	for _, finding := range findings {
		out = append(out, model.Finding{Claim: finding.Claim, Evidence: append([]string(nil), finding.Evidence...)})
	}
	return out
}

func resultID(result model.TaskResult) string {
	payload, err := json.Marshal(struct {
		SchemaVersion int
		AgentID       string
		ChildRunID    string
		Status        model.ResultStatus
		Summary       string
		Findings      []model.Finding
		Artifacts     []model.Artifact
		ErrorCode     string
		Usage         model.Usage
		Truncated     bool
		Omitted       []string
		NextAction    string
	}{
		SchemaVersion: result.SchemaVersion,
		AgentID:       result.AgentID,
		ChildRunID:    result.ChildRunID,
		Status:        result.Status,
		Summary:       result.Summary,
		Findings:      result.Findings,
		Artifacts:     result.Artifacts,
		ErrorCode:     errorCode(result.Error),
		Usage:         result.Usage,
		Truncated:     result.Truncated,
		Omitted:       result.OmittedSections,
		NextAction:    result.NextAction,
	})
	if err != nil {
		payload = []byte(fmt.Sprintf("v%d|%s|%s|%s", result.SchemaVersion, result.AgentID, result.ChildRunID, result.Status))
	}
	digest := sha256.Sum256(payload)
	return "result-" + hex.EncodeToString(digest[:12])
}

func errorCode(err *model.ResultError) string {
	if err == nil {
		return ""
	}
	return err.Code
}

func errorCodeFor(err error) string {
	if err == nil {
		return "subagent_failed"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "budget exceeded"):
		return "budget_exceeded"
	case strings.Contains(message, "deadline exceeded") || strings.Contains(message, "timeout"):
		return "timeout"
	default:
		return "subagent_failed"
	}
}

func retryable(err error) bool {
	if err == nil {
		return true
	}
	return !strings.Contains(strings.ToLower(err.Error()), "permission denied")
}
