package react

import (
	"encoding/json"
	"path"
	"strings"

	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
)

const deliveryHintText = "artifacts[].path is already the controlled delivery path; do not copy/write_file the artifact again to $OUTPUT_DIR or workspace root; do not forge binary documents via write_file"

// annotateSkillFollowHints 按已加载技能正文做软门禁提示，不改变成功/失败语义。
// 规则全部来自技能正文结构（前置章节链接、QA 章节命令），不绑定具体技能名或产物类型。
func annotateSkillFollowHints(rc *runtime.RunContext, toolName, args, content string) string {
	if rc == nil || rc.SkillFollow == nil {
		return content
	}
	follow := rc.SkillFollow
	name := strings.TrimSpace(toolName)

	switch name {
	case "read_skill_resource", "read_file":
		if rel := extractReadTarget(args, content); rel != "" {
			follow.MarkResourceRead(rel)
		}
	case "run_skill_command":
		if cmd := extractCommandArg(args); cmd != "" {
			follow.NoteExecutedCommand(cmd, toolResultOK(content))
		}
		if toolResultOK(content) {
			if names := extractDeliveredBasenames(content); len(names) > 0 {
				follow.NoteDeliveredArtifacts(names)
			}
		}
	}

	hints := map[string]any{}
	cmd := extractCommandArg(args)
	isQA := cmd != "" && follow.IsQACommand(cmd)

	if (name == "write_file" || name == "run_skill_command") && !isQA {
		if unread := follow.UnreadCreatingRequired(); len(unread) > 0 {
			hints["prerequisites_unread"] = unread
			hints["skill_follow"] = "prerequisites_unread"
			hints["warning"] = "DO NOT proceed with creation until these skill guides are read via read_skill_resource: " + strings.Join(unread, ", ")
			hints["prerequisites_hint"] = "Skill requires reading linked .md guides in Creating/Workflow/Procedure/Design sections before writing or running work commands; read them via skill resource tools first (short names like design.md are OK when skill name is set)."
		}
	}
	if name == "run_skill_command" && isQA && !toolResultOK(content) {
		hints["qa_failed"] = true
		hints["skill_follow"] = "qa_failed"
		if kind := extractFailureKind(content); kind != "" {
			hints["qa_failure_kind"] = kind
		}
		hints["qa_hint"] = "Skill QA command failed; do not claim visual/content QA complete. If failure_kind=dependency_missing (system), report environment gap and finish with incomplete delivery; do not invent QA screenshots."
	}
	if name == "run_skill_command" && follow.RequiresQA() && !follow.QADone() && !isQA && hasProducedArtifact(content) {
		hints["qa_pending"] = true
		pending := follow.PendingQACommands()
		if len(pending) == 0 {
			pending = follow.QACommands()
		}
		if len(pending) > 0 {
			hints["qa_hint"] = "Skill requires QA before finishing; still pending via run_skill_command: " + strings.Join(pending, "; ")
		} else {
			hints["qa_hint"] = "Skill requires QA before finishing; run the QA steps declared in the skill via run_skill_command and confirm results."
		}
	}
	if name == "run_skill_command" && hasProducedArtifact(content) && toolResultOK(content) {
		hints["delivery_hint"] = deliveryHintText
		hints["warning"] = "Artifact already delivered at artifacts[].path — treat that path as final; do not re-copy with write_file."
	}
	if name == "write_file" && shouldWarnRedeivery(follow, args, content) {
		hints["delivery_hint"] = deliveryHintText
		hints["skill_follow"] = "delivery_complete"
		hints["warning"] = "Do not re-deliver or forge artifacts with write_file; use artifacts[].path from the earlier run_skill_command result."
	}
	if len(hints) == 0 {
		return content
	}
	return mergeJSONHints(content, hints)
}

func shouldWarnRedeivery(follow *runtime.SkillFollowState, args, content string) bool {
	if looksLikeArtifactInvalid(content) {
		return true
	}
	writePath := extractWritePath(args)
	if writePath == "" {
		return false
	}
	lower := strings.ToLower(strings.ReplaceAll(writePath, `\`, `/`))
	if strings.Contains(lower, "$output_dir") || strings.Contains(lower, "${output_dir}") ||
		strings.Contains(lower, "%output_dir%") || strings.Contains(lower, "/output/") {
		return true
	}
	base := path.Base(lower)
	if follow != nil && follow.IsDeliveredName(base) {
		return true
	}
	if follow != nil && follow.HasDeliveredArtifacts() && looksLikeBinaryArtifactName(base) {
		return true
	}
	return false
}

func looksLikeArtifactInvalid(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "artifact_invalid") || strings.Contains(content, "冒充")
}

func looksLikeBinaryArtifactName(base string) bool {
	switch path.Ext(base) {
	case ".pptx", ".ppt", ".docx", ".doc", ".xlsx", ".xls", ".pdf", ".zip", ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func extractWritePath(args string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return ""
	}
	if v, ok := raw["path"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func extractDeliveredBasenames(content string) []string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(name string) {
		base := strings.ToLower(path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, `/`)))
		if base == "" || base == "." {
			return
		}
		if _, ok := seen[base]; ok {
			return
		}
		seen[base] = struct{}{}
		out = append(out, base)
	}
	if arts, ok := raw["artifacts"].([]any); ok {
		for _, item := range arts {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if n, ok := m["name"].(string); ok {
				add(n)
			} else if p, ok := m["path"].(string); ok {
				add(p)
			}
		}
	}
	if produced, ok := raw["produced"].([]any); ok {
		for _, item := range produced {
			if s, ok := item.(string); ok {
				add(s)
			}
		}
	}
	return out
}

func extractReadTarget(args, content string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err == nil {
		for _, key := range []string{"resource", "path", "name"} {
			if v, ok := raw[key].(string); ok && strings.TrimSpace(v) != "" {
				if strings.HasSuffix(strings.ToLower(v), ".md") || key == "resource" {
					return v
				}
			}
		}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(content), &out); err == nil {
		if v, ok := out["resource"].(string); ok {
			return v
		}
		if v, ok := out["path"].(string); ok && strings.HasSuffix(strings.ToLower(v), ".md") {
			return v
		}
	}
	return ""
}

func extractCommandArg(args string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return ""
	}
	if v, ok := raw["command"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func toolResultOK(content string) bool {
	var raw map[string]any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		// 非 JSON 结果：不视为成功 QA（避免误清 pending）
		return false
	}
	if ok, exists := raw["ok"].(bool); exists {
		return ok
	}
	if errVal, exists := raw["error"]; exists {
		switch v := errVal.(type) {
		case string:
			return strings.TrimSpace(v) == ""
		case nil:
			return true
		default:
			return false
		}
	}
	return true
}

func extractFailureKind(content string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return ""
	}
	if v, ok := raw["failure_kind"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func hasProducedArtifact(content string) bool {
	var raw map[string]any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return false
	}
	if arts, ok := raw["artifacts"].([]any); ok && len(arts) > 0 {
		return true
	}
	if produced, ok := raw["produced"].([]any); ok && len(produced) > 0 {
		return true
	}
	if p, ok := raw["path"].(string); ok && strings.TrimSpace(p) != "" && looksLikeArtifactPath(p) {
		return true
	}
	return false
}

func looksLikeArtifactPath(p string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(p, `\`, `/`)))
	if base == "" || base == "." {
		return false
	}
	return strings.Contains(base, ".")
}

func mergeJSONHints(content string, hints map[string]any) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(content), &raw); err != nil || raw == nil {
		payload := map[string]any{"result": content}
		for k, v := range hints {
			payload[k] = v
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return content
		}
		return string(data)
	}
	for k, v := range hints {
		raw[k] = v
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return content
	}
	return string(data)
}

// registerSkillInjectionFollow 在注入技能时登记跟踪状态。
func registerSkillInjectionFollow(rc *runtime.RunContext, content string) {
	if rc == nil {
		return
	}
	if rc.SkillFollow == nil {
		rc.SkillFollow = runtime.NewSkillFollowState()
	}
	rc.SkillFollow.RegisterInjection(content)
}

// applySkillFollowIncomplete 终态：已交付但技能 QA 未成功完成时标 Incomplete（三端共用）。
func applySkillFollowIncomplete(rc *runtime.RunContext, log logger.Logger) bool {
	if rc == nil || rc.Run == nil || rc.SkillFollow == nil {
		return false
	}
	if !rc.SkillFollow.IncompleteDelivery() {
		return false
	}
	rc.Run.Incomplete = true
	pending := rc.SkillFollow.PendingQACommands()
	if log != nil {
		log.Warn("Skill QA 未完成，Run 标为 Incomplete",
			"pending_qa", strings.Join(pending, "; "),
			"delivered", true,
		)
	}
	return true
}
