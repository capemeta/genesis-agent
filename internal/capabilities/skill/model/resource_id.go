package model

import (
	"path"
	"strings"
)

// QualifySkillResource 将模型常用的短 resource 归一成包内 ResourceID。
// 通用规则（不绑定具体技能）：
//   - 已带 package 前缀：保持
//   - bare 文件名（design.md）：→ <package>/design.md
//   - references|assets|scripts/...：→ <package>/references/...
// packageID 优先；为空时用 skillName（通常即包名/技能名）。
func QualifySkillResource(packageID, skillName, resource string) ResourceID {
	raw := strings.TrimSpace(strings.ReplaceAll(resource, `\`, `/`))
	raw = strings.TrimPrefix(raw, "./")
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return ""
	}
	pkg := strings.TrimSpace(packageID)
	if pkg == "" {
		pkg = strings.TrimSpace(skillName)
	}
	// qualified_name 可能是 ns/name，包目录一般取最后一段
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return ResourceID(path.Clean(raw))
	}
	if raw == pkg || strings.EqualFold(raw, "skill.md") {
		return ResourceID(path.Clean(pkg + "/SKILL.md"))
	}
	if strings.HasPrefix(raw, pkg+"/") {
		return ResourceID(path.Clean(raw))
	}
	parts := strings.Split(raw, "/")
	if len(parts) == 1 {
		return ResourceID(path.Clean(pkg + "/" + raw))
	}
	switch strings.ToLower(parts[0]) {
	case "references", "assets", "scripts":
		return ResourceID(path.Clean(pkg + "/" + raw))
	default:
		// 已是其他包前缀或完整相对路径，原样清理
		return ResourceID(path.Clean(raw))
	}
}

// ResourceLookupCandidates 返回索引/匹配时可能用到的 resource 形态（短名与限定名）。
func ResourceLookupCandidates(packageID, skillName, resource string) []string {
	raw := strings.TrimSpace(strings.ReplaceAll(resource, `\`, `/`))
	raw = strings.Trim(strings.TrimPrefix(raw, "./"), "/")
	qualified := string(QualifySkillResource(packageID, skillName, resource))
	seen := map[string]struct{}{}
	out := make([]string, 0, 3)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	add(raw)
	add(qualified)
	if qualified != "" {
		add(path.Base(qualified))
	}
	return out
}
