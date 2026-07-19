package service

import (
	"encoding/json"
	"regexp"
	"strings"
)

var visualChecklistRe = regexp.MustCompile(`(?i)\[VISUAL_CHECKLIST:\s*([^\]]+)\]`)

// VisualSignOff 是可校验的视觉 QA 断言结果。
type VisualSignOff struct {
	Passed  bool
	Fields  map[string]string
	Defects []string
	Raw     string
}

// ParseVisualChecklist 解析形态 A 的 `[VISUAL_CHECKLIST: k=v, ...]` 回执。
// 字段齐全且无 reject/fail/bad 取值时 Passed=true。
func ParseVisualChecklist(text string) (VisualSignOff, bool) {
	m := visualChecklistRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return VisualSignOff{}, false
	}
	fields := map[string]string{}
	for _, part := range strings.Split(m[1], ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := strings.ToLower(strings.TrimSpace(kv[1]))
		fields[k] = v
	}
	if len(fields) == 0 {
		return VisualSignOff{}, false
	}
	required := []string{"layout", "contrast", "overflow"}
	for _, k := range required {
		if _, ok := fields[k]; !ok {
			return VisualSignOff{Fields: fields, Raw: m[0], Passed: false}, true
		}
	}
	passed := true
	var defects []string
	for k, v := range fields {
		switch v {
		case "ok", "none", "pass", "passed", "good":
		case "reject", "fail", "failed", "bad", "error":
			passed = false
			defects = append(defects, k+"="+v)
		default:
			// 未知取值不放行
			passed = false
			defects = append(defects, k+"="+v)
		}
	}
	return VisualSignOff{Passed: passed, Fields: fields, Defects: defects, Raw: m[0]}, true
}

// ParseExpertVisualJSON 解析形态 B 专家返回的 JSON（允许夹杂前后文本）。
func ParseExpertVisualJSON(text string) (VisualSignOff, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return VisualSignOff{}, false
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return VisualSignOff{}, false
	}
	var payload struct {
		Passed  bool     `json:"passed"`
		Defects []string `json:"defects"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &payload); err != nil {
		return VisualSignOff{}, false
	}
	passed := payload.Passed && len(payload.Defects) == 0
	return VisualSignOff{Passed: passed, Defects: payload.Defects, Raw: text[start : end+1]}, true
}
