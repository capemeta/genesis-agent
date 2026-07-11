package repeatguard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

// 默认忽略字段：易变身份噪声，不参与 call_key。
var defaultIgnoreFields = map[string]struct{}{
	"request_id":         {},
	"trace_id":           {},
	"timestamp":          {},
	"nonce":              {},
	"client_request_id":  {},
}

// PathRoots 用于将绝对路径改写为逻辑前缀，避免同文件不同绝对路径拆成多个 key。
type PathRoots struct {
	WorkDir   string
	InputDir  string
	OutputDir string
	TmpDir    string
	SkillDir  string
}

// CallIdentity 描述一次工具调用的规范化身份。
type CallIdentity struct {
	ToolName   string
	CallKey    string
	Canonical  string // 规范化后的 args 摘要（调试/日志）
	KeyPrefix  string // call_key 前 12 位十六进制
}

// BuildCallKey 计算 call_key = hash(tool + "\0" + canonical_json(normalize(args)))。
func BuildCallKey(toolName, argsJSON string, roots PathRoots, extraIgnore []string) CallIdentity {
	tool := strings.TrimSpace(toolName)
	canonical := normalizeArgs(argsJSON, roots, extraIgnore)
	sum := sha256.Sum256([]byte(tool + "\x00" + canonical))
	key := hex.EncodeToString(sum[:])
	prefix := key
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	return CallIdentity{
		ToolName:  tool,
		CallKey:   key,
		Canonical: truncateRunes(canonical, 240),
		KeyPrefix: prefix,
	}
}

func normalizeArgs(argsJSON string, roots PathRoots, extraIgnore []string) string {
	trimmed := strings.TrimSpace(argsJSON)
	if trimmed == "" {
		return ""
	}
	var raw any
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return trimmed
	}
	ignore := mergeIgnore(extraIgnore)
	normalized := normalizeValue(raw, roots, ignore)
	data, err := json.Marshal(normalized)
	if err != nil {
		return trimmed
	}
	return string(data)
}

func mergeIgnore(extra []string) map[string]struct{} {
	out := make(map[string]struct{}, len(defaultIgnoreFields)+len(extra))
	for k := range defaultIgnoreFields {
		out[k] = struct{}{}
	}
	for _, k := range extra {
		k = strings.TrimSpace(strings.ToLower(k))
		if k != "" {
			out[k] = struct{}{}
		}
	}
	return out
}

func normalizeValue(v any, roots PathRoots, ignore map[string]struct{}) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			if _, skip := ignore[strings.ToLower(k)]; skip {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// 用有序 slice 再转回 map 前先构建稳定结构：json.Marshal map 本身无序，
		// 因此这里输出为按 key 排序的「伪 object」——通过 json.Marshal 对 map 不稳定。
		// 改为返回 []any{key, value, ...} 会破坏可读性；改用有序 encoding。
		ordered := make(map[string]any, len(keys))
		for _, k := range keys {
			ordered[k] = normalizeValue(t[k], roots, ignore)
		}
		// 为保证稳定哈希，手动按排序 key 拼 JSON object。
		return orderedMap{m: ordered, keys: keys}
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = normalizeValue(item, roots, ignore)
		}
		return out
	case string:
		s := strings.TrimSpace(t)
		return rewritePath(s, roots)
	case float64, bool, nil:
		return t
	default:
		return t
	}
}

// orderedMap 在 Marshal 时按给定 key 顺序输出，保证 call_key 稳定。
type orderedMap struct {
	m    map[string]any
	keys []string
}

func (o orderedMap) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		valJSON, err := json.Marshal(o.m[k])
		if err != nil {
			return nil, err
		}
		b.Write(keyJSON)
		b.WriteByte(':')
		b.Write(valJSON)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func rewritePath(s string, roots PathRoots) string {
	if s == "" || rootsEmpty(roots) {
		return s
	}
	type root struct {
		prefix string
		label  string
	}
	candidates := []root{
		{roots.OutputDir, "$OUTPUT_DIR"},
		{roots.InputDir, "$INPUT_DIR"},
		{roots.SkillDir, "$SKILL_DIR"},
		{roots.TmpDir, "$TMPDIR"},
		{roots.WorkDir, "$WORK_DIR"},
	}
	// 长前缀优先，避免 WORK_DIR 吞掉子目录。
	sort.SliceStable(candidates, func(i, j int) bool {
		return len(candidates[i].prefix) > len(candidates[j].prefix)
	})
	clean := filepath.Clean(s)
	for _, c := range candidates {
		if strings.TrimSpace(c.prefix) == "" {
			continue
		}
		rootClean := filepath.Clean(c.prefix)
		if rel, err := filepath.Rel(rootClean, clean); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return c.label
			}
			return c.label + "/" + rel
		}
	}
	return s
}

func rootsEmpty(r PathRoots) bool {
	return strings.TrimSpace(r.WorkDir) == "" &&
		strings.TrimSpace(r.InputDir) == "" &&
		strings.TrimSpace(r.OutputDir) == "" &&
		strings.TrimSpace(r.TmpDir) == "" &&
		strings.TrimSpace(r.SkillDir) == ""
}

func truncateRunes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
