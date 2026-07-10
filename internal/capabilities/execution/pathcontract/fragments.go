package pathcontract

import (
	"regexp"
	"strings"
)

var pathLikePattern = regexp.MustCompile(`(?i)(\$?\{?(?:INPUT_DIR|OUTPUT_DIR|TMPDIR|WORK_DIR|SKILL_DIR|GENESIS_WORKSPACE)\}?[/\\][^\s"'` + "`" + `;|&)]*|%?(?:INPUT_DIR|OUTPUT_DIR|TMPDIR|WORK_DIR|SKILL_DIR|GENESIS_WORKSPACE)%?[/\\][^\s"'` + "`" + `;|&)]*|[a-z]:[/\\][^\s"'` + "`" + `;|&)]*|\\\\[^\s"'` + "`" + `;|&)]*|/[^\s"'` + "`" + `;|&)]*)`)
var windowsAbsPattern = regexp.MustCompile(`^[a-z]:/`)

func violationsFromText(analyzer, location, text string) []Violation {
	matches := pathFragmentsInText(text)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	violations := make([]Violation, 0, len(matches))
	for _, raw := range matches {
		fragment := trimPathFragment(raw)
		if fragment == "" || seen[fragment] || allowedStrictFragment(fragment) {
			continue
		}
		seen[fragment] = true
		v := violationFor(fragment, location)
		v.Analyzer = analyzer
		violations = append(violations, v)
	}
	return violations
}

func pathFragmentsInText(text string) []string {
	indexes := pathLikePattern.FindAllStringIndex(text, -1)
	if len(indexes) == 0 {
		return nil
	}
	fragments := make([]string, 0, len(indexes))
	for _, index := range indexes {
		start, end := index[0], index[1]
		if start > 0 && text[start-1] == ':' {
			continue
		}
		fragment := text[start:end]
		if start > 0 && len(fragment) > 2 && fragment[1] == ':' && isASCIILetter(text[start-1]) {
			continue
		}
		if strings.HasPrefix(fragment, "/") && start > 0 && !isPathBoundary(text[start-1]) {
			continue
		}
		fragments = append(fragments, fragment)
	}
	return fragments
}

func trimPathFragment(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, `"'`)
	raw = strings.TrimRight(raw, `,.:`)
	return raw
}

func allowedStrictFragment(fragment string) bool {
	normalized := strings.ReplaceAll(fragment, `\`, `/`)
	lower := strings.ToLower(normalized)
	switch {
	case strings.HasPrefix(lower, "$input_dir/"),
		strings.HasPrefix(lower, "${input_dir}/"),
		strings.HasPrefix(lower, "%input_dir%/"),
		strings.HasPrefix(lower, "$output_dir/"),
		strings.HasPrefix(lower, "${output_dir}/"),
		strings.HasPrefix(lower, "%output_dir%/"),
		strings.HasPrefix(lower, "$tmpdir/"),
		strings.HasPrefix(lower, "${tmpdir}/"),
		strings.HasPrefix(lower, "%tmpdir%/"),
		strings.HasPrefix(lower, "$work_dir/"),
		strings.HasPrefix(lower, "${work_dir}/"),
		strings.HasPrefix(lower, "%work_dir%/"),
		strings.HasPrefix(lower, "$skill_dir/"),
		strings.HasPrefix(lower, "${skill_dir}/"),
		strings.HasPrefix(lower, "%skill_dir%/"),
		strings.HasPrefix(lower, "$genesis_workspace/"),
		strings.HasPrefix(lower, "${genesis_workspace}/"),
		strings.HasPrefix(lower, "%genesis_workspace%/"):
		return true
	case lower == "/workspace",
		strings.HasPrefix(lower, "/workspace/"):
		return true
	default:
		return false
	}
}

func violationFor(fragment, location string) Violation {
	normalized := strings.ReplaceAll(fragment, `\`, `/`)
	lower := strings.ToLower(normalized)
	switch {
	case windowsAbsPattern.MatchString(lower),
		strings.HasPrefix(lower, "//"),
		strings.HasPrefix(lower, "/users/"),
		strings.HasPrefix(lower, "/home/"),
		strings.HasPrefix(lower, "/mnt/"),
		strings.HasPrefix(lower, "/volumes/"):
		return Violation{
			Severity: SeverityError,
			Fragment: fragment,
			Location: location,
			Reason:   "sandbox/任务型执行不能直接访问宿主机绝对路径",
			Fix:      "先通过输入 staging 把文件放入 INPUT_DIR，再在代码中读取 INPUT_DIR 下的文件",
		}
	case strings.HasPrefix(lower, "/tmp/"):
		return Violation{
			Severity: SeverityError,
			Fragment: fragment,
			Location: location,
			Reason:   "最终成果或跨步骤状态不能写入 /tmp",
			Fix:      "临时文件使用 TMPDIR，最终成果写入 OUTPUT_DIR，需要跨 job 复用的状态写入 WORK_DIR",
		}
	default:
		return Violation{
			Severity: SeverityError,
			Fragment: fragment,
			Location: location,
			Reason:   "strict workspace contract 下不允许使用非标准绝对路径",
			Fix:      "输入使用 INPUT_DIR，成果使用 OUTPUT_DIR，临时文件使用 TMPDIR，跨 job 状态使用 WORK_DIR",
		}
	}
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isPathBoundary(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '"', '\'', '`', '(', '[', '{', '=', ':', ',':
		return true
	default:
		return false
	}
}
