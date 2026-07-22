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
		code := errorCodeFor(candidate.Err)
		result.Error = &model.ResultError{Code: code, Message: r.sanitizeError(candidate.Err), Retryable: retryable(candidate.Err)}
		result.Summary, result.Truncated = r.sanitizeAndTruncate(finalAnswer(candidate.Run), r.limit())
		if result.Summary == "" {
			result.Summary = result.Error.Message
		}
		result.NextAction = nextActionForFailure(code)
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
			// 父交付面只暴露交付候选：QA 预览/中间物（role=qa_asset）留在子执行面，父根本收不到（spec §7.2 C1）。
			result.Artifacts = deliverablesForParent(evidence.Artifacts)
		}
		// R1=A：子智能体的交付通常已在其子 Run 内 finalize+deliver；父 Agent 只总结，
		// 禁止再 glob/探测子 cwd/手动 select（跨 Run select 会 ADOPTION_REQUIRED）。故此处不再产出 select 提示。
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
	return PassthroughEvidenceValidator{}
}

// deliverablesForParent 只保留交付候选，剔除 QA/中间物（role=qa_asset）；
// 同名/同 Path 只保留最后一次登记（改稿后旧版不进父面）；并补齐 CandidateID/ResourceID 互填。
func deliverablesForParent(artifacts []model.Artifact) []model.Artifact {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]model.Artifact, 0, len(artifacts))
	indexByKey := map[string]int{}
	for _, art := range artifacts {
		if art.Role == model.ArtifactRoleQAAsset {
			continue
		}
		if art.CandidateID == "" {
			art.CandidateID = art.ResourceID
		}
		if art.ResourceID == "" {
			art.ResourceID = art.CandidateID
		}
		key := artifactDedupeKey(art)
		if key != "" {
			if i, ok := indexByKey[key]; ok {
				out[i] = art
				continue
			}
			indexByKey[key] = len(out)
		}
		out = append(out, art)
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
	case strings.Contains(err.Error(), "RUN_COMPLETION_REQUIRED"):
		// 子已跑到终点但未自证完成门禁（如视觉 QA / 交付门禁）。这类失败原样重委派通常会复现，
		// 单列一类以便父侧给出「补能力/调整任务」而非盲目重委派的指引，避免无效循环。
		return "completion_gate_unmet"
	default:
		return "subagent_failed"
	}
}

// nextActionForFailure 按失败类别给父 Agent 差异化下一步指引。
// completion_gate_unmet 明确劝阻「原样重新委派」，因为门禁不满足通常源于子缺能力或任务本身，
// 重复委派会复现同样失败（本类回归的无效循环根因）。
func nextActionForFailure(code string) string {
	if code == "completion_gate_unmet" {
		return "子已产出但未通过完成门禁（如视觉 QA / 交付门禁），原样重新委派通常会复现同样失败。" +
			"请先确认子 Agent 具备门禁所需能力（如 view_image 视觉 QA 工具），或调整任务/门禁后再委派；不要重复委派相同任务。"
	}
	return "可重新委派、调整任务或稍后 resume。"
}

func retryable(err error) bool {
	if err == nil {
		return true
	}
	return !strings.Contains(strings.ToLower(err.Error()), "permission denied")
}
