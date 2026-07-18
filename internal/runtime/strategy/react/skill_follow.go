package react

import (
	"encoding/json"
	"strings"

	"genesis-agent/internal/runtime"
)

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
		follow.NoteQAEnvironmentFailure(cmd, extractFailureKind(content))
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
	if len(hints) == 0 {
		return content
	}
	return mergeJSONHints(content, hints)
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
	if !decodeFirstJSONObject(content, &raw) {
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
	if !decodeFirstJSONObject(content, &raw) {
		return ""
	}
	if v, ok := raw["failure_kind"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// decodeFirstJSONObject 接受 Tool Gateway 在结构化结果后追加的人类可读错误摘要。
// 只读取首个 JSON 对象，不把尾随诊断误当成协议字段。
func decodeFirstJSONObject(content string, target *map[string]any) bool {
	if target == nil {
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(content)))
	return decoder.Decode(target) == nil && *target != nil
}

func hasProducedArtifact(content string) bool {
	var raw map[string]any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return false
	}
	if produced, ok := raw["produced"].([]any); ok && len(produced) > 0 {
		return true
	}
	return false
}

func mergeJSONHints(content string, hints map[string]any) string {
	var raw map[string]any
	trimmed := strings.TrimSpace(content)
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	if err := decoder.Decode(&raw); err != nil || raw == nil {
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
	if offset := decoder.InputOffset(); offset < int64(len(trimmed)) {
		if diagnostic := strings.TrimSpace(trimmed[offset:]); diagnostic != "" {
			raw["diagnostic"] = diagnostic
		}
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
