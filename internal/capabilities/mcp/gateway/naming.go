package gateway

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	mcpNamePrefix   = "mcp__"
	maxModelNameLen = 64
)

var nonIdent = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// SanitizeIdentifier 将非法字符替换为 `_`（对齐 Kode sanitizeMcpIdentifierPart）。
func SanitizeIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	out := nonIdent.ReplaceAllString(value, "_")
	if out == "" {
		return "_"
	}
	return out
}

// ModelToolName 生成模型可见名：mcp__{sanitize(server)}__{sanitize(tool)}。
func ModelToolName(server, tool string) string {
	return mcpNamePrefix + SanitizeIdentifier(server) + "__" + SanitizeIdentifier(tool)
}

// Deduper 保证模型可见名唯一（超长截断 + 数字后缀，先不引入 SHA1）。
type Deduper struct {
	used map[string]struct{}
}

// NewDeduper 创建去重器。
func NewDeduper() *Deduper {
	return &Deduper{used: make(map[string]struct{})}
}

// Unique 返回唯一模型可见名。
func (d *Deduper) Unique(server, tool string) string {
	base := ModelToolName(server, tool)
	if len(base) > maxModelNameLen {
		base = base[:maxModelNameLen]
	}
	name := base
	for i := 2; ; i++ {
		if _, ok := d.used[name]; !ok {
			d.used[name] = struct{}{}
			return name
		}
		suffix := fmt.Sprintf("_%d", i)
		trim := maxModelNameLen - len(suffix)
		if trim < 1 {
			trim = 1
		}
		candidate := base
		if len(candidate) > trim {
			candidate = candidate[:trim]
		}
		name = candidate + suffix
	}
}
