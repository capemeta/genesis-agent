package repeatguard

import (
	"encoding/json"
	"strings"
)

// Outcome 描述一次真实工具执行对 Guard 可见的结果。
type Outcome struct {
	Success         bool
	FailureKind     string
	SuggestedAction string
	ErrorExcerpt    string
	ResultExcerpt   string
	Artifacts       []string
	Skill           string // install / run 结果中的 skill 名（若有）
}

// ParseOutcome 从工具返回内容与 error 解析成功/失败语义。
// err != nil 一律视为失败；JSON 含 ok=false 视为失败；其余 nil err 视为成功。
func ParseOutcome(toolName, result string, toolErr error) Outcome {
	out := Outcome{
		ResultExcerpt: truncateRunes(strings.TrimSpace(result), 400),
	}
	trimmed := strings.TrimSpace(result)
	var payload map[string]any
	if trimmed != "" {
		_ = json.Unmarshal([]byte(trimmed), &payload)
	}

	if payload != nil {
		out.FailureKind = stringField(payload, "failure_kind")
		out.SuggestedAction = firstNonEmpty(
			stringField(payload, "suggested_action"),
			stringField(payload, "suggested_next"),
		)
		out.Skill = stringField(payload, "skill")
		if out.ErrorExcerpt == "" {
			out.ErrorExcerpt = firstNonEmpty(
				stringField(payload, "error"),
				stringField(payload, "stderr"),
				stringField(payload, "message"),
			)
		}
		out.Artifacts = extractArtifacts(toolName, payload)
		if meta, ok := payload["metadata"].(map[string]any); ok {
			if out.FailureKind == "" {
				out.FailureKind = stringField(meta, "failure_kind")
			}
			if out.Skill == "" {
				out.Skill = stringField(meta, "skill")
			}
		}
	}

	if toolErr != nil {
		out.Success = false
		if out.FailureKind == "" {
			out.FailureKind = inferFailureKindFromError(toolErr.Error())
		}
		if out.ErrorExcerpt == "" {
			out.ErrorExcerpt = truncateRunes(toolErr.Error(), 400)
		}
		if out.FailureKind == "" {
			out.FailureKind = "tool_error"
		}
		return out
	}

	if payload != nil {
		if okVal, hasOK := payload["ok"]; hasOK {
			if b, isBool := okVal.(bool); isBool {
				out.Success = b
				if !b && out.FailureKind == "" {
					out.FailureKind = "tool_error"
				}
				return out
			}
		}
		if out.FailureKind != "" {
			out.Success = false
			return out
		}
	}

	out.Success = true
	return out
}

func extractArtifacts(toolName string, payload map[string]any) []string {
	var out []string
	switch strings.TrimSpace(toolName) {
	case "write_file", "apply_patch":
		if p := stringField(payload, "path"); p != "" {
			out = append(out, p)
		}
	}
	if arts, ok := payload["artifacts"].([]any); ok {
		for _, a := range arts {
			switch t := a.(type) {
			case string:
				if s := strings.TrimSpace(t); s != "" {
					out = append(out, s)
				}
			case map[string]any:
				if p := firstNonEmpty(stringField(t, "path"), stringField(t, "name")); p != "" {
					out = append(out, p)
				}
			}
		}
	}
	if p := stringField(payload, "output_path"); p != "" {
		out = append(out, p)
	}
	return out
}

func inferFailureKindFromError(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "unexpected eof"), strings.Contains(lower, "unexpected end of json input"), strings.Contains(lower, "unterminated string"):
		return "tool_arguments_truncated"
	case strings.Contains(lower, "path_contract") || strings.Contains(lower, "execution_path_contract"):
		return "path_contract_violation"
	case strings.Contains(lower, "dependency") || strings.Contains(lower, "cannot find module") || strings.Contains(lower, "modulenotfound"):
		return "dependency_missing"
	case strings.Contains(lower, "approval"):
		return "approval_denied"
	case strings.Contains(lower, "sandbox"):
		return "sandbox_violation"
	case strings.Contains(lower, "timeout"):
		return "timeout"
	default:
		return ""
	}
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		data, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func skillFromArgs(argsJSON string) string {
	trimmed := strings.TrimSpace(argsJSON)
	if trimmed == "" {
		return ""
	}
	var payload map[string]any
	if json.Unmarshal([]byte(trimmed), &payload) != nil {
		return ""
	}
	return stringField(payload, "skill")
}
