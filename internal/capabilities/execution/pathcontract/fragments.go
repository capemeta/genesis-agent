package pathcontract

import (
	"regexp"
	"strings"
)

var pathLikePattern = regexp.MustCompile(`(?i)(\$?\{?(?:INPUT_DIR|OUTPUT_DIR|TMPDIR|WORK_DIR|SKILL_DIR|GENESIS_WORKSPACE)\}?[/\\][^\s"'` + "`" + `;|&)]*|%?(?:INPUT_DIR|OUTPUT_DIR|TMPDIR|WORK_DIR|SKILL_DIR|GENESIS_WORKSPACE)%?[/\\][^\s"'` + "`" + `;|&)]*|[a-z]:[/\\][^\s"'` + "`" + `;|&)]*|\\\\[^\s"'` + "`" + `;|&)]*|/[A-Za-z0-9._~${%][^\s"'` + "`" + `;|&)]*)`)
var windowsAbsPattern = regexp.MustCompile(`^[a-z]:/`)

// pathScanMode 控制路径片段放行策略。
// shell 命令行保持严格；源码字面量允许「系统工具探测根」（which/PROGRAMFILES 回退），
// 避免把 LibreOffice 等安装目录探测误判为业务访问宿主机路径。
type pathScanMode int

const (
	pathScanStrict pathScanMode = iota
	pathScanSource
)

func violationsFromText(analyzer, location, text string) []Violation {
	return violationsFromTextMode(analyzer, location, text, pathScanStrict)
}

func violationsFromTextMode(analyzer, location, text string, mode pathScanMode) []Violation {
	matches := pathFragmentsInText(text)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	violations := make([]Violation, 0, len(matches))
	for _, raw := range matches {
		fragment := trimPathFragment(raw)
		if fragment == "" || seen[fragment] || !isMeaningfulPathFragment(fragment) || allowedStrictFragment(fragment) {
			continue
		}
		if mode == pathScanSource && isSystemToolDiscoveryFragment(fragment) {
			continue
		}
		seen[fragment] = true
		v := violationFor(fragment, location)
		v.Analyzer = analyzer
		violations = append(violations, v)
	}
	return violations
}

// isSystemToolDiscoveryFragment 识别源码中常见的系统工具/字体安装与探测路径。
// 路径契约扫描器在空格处截断，故 "C:\\Program Files\\..." 可能只剩 "C:\\Program"；
// 字体探测同理（如 "/System/Library/Fonts/STHeiti Light.ttc" 可能截成 ".../STHeiti"）。
func isSystemToolDiscoveryFragment(fragment string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(fragment), `\`, `/`))
	if windowsAbsPattern.MatchString(normalized) {
		rest := normalized[2:] // 去掉 "c:"
		if strings.HasPrefix(rest, "/program") {
			after := strings.TrimPrefix(rest, "/program")
			// C:/Program | C:/Program Files | C:/Program Files (x86)/...
			if after == "" || strings.HasPrefix(after, " files") {
				return true
			}
		}
		// Windows 字体目录探测（reportlab CJK 等）
		if rest == "/windows" || strings.HasPrefix(rest, "/windows/fonts") {
			return true
		}
	}
	unixRoots := []string{
		"/usr/bin/",
		"/usr/local/bin/",
		"/bin/",
		"/opt/",
		"/usr/share/fonts/",
		"/library/fonts/",
		"/system/library/fonts/",
	}
	for _, root := range unixRoots {
		if strings.HasPrefix(normalized, root) || normalized == strings.TrimSuffix(root, "/") {
			return true
		}
	}
	return false
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
	// OOXML 包内相对路径（/ppt/ /word/ /xl/），不是宿主机文件系统路径。
	case isOfficePackagePath(lower):
		return true
	default:
		return false
	}
}

// isMeaningfulPathFragment 过滤「office-basic / Node」这类自然语言斜杠，以及单独的 "/"。
func isMeaningfulPathFragment(fragment string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(fragment), `\`, `/`)
	if normalized == "" || normalized == "/" {
		return false
	}
	// UNC：//server/share
	if strings.HasPrefix(normalized, "//") {
		return len(normalized) > 2
	}
	// Windows 盘符：c:/...
	if windowsAbsPattern.MatchString(strings.ToLower(normalized)) {
		return true
	}
	// Unix 绝对路径至少是 /x；裸 "/" 不算。
	if strings.HasPrefix(normalized, "/") {
		if len(normalized) < 2 {
			return false
		}
		first := normalized[1]
		if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') ||
			(first >= '0' && first <= '9') || first == '.' || first == '_' || first == '~' ||
			first == '$' || first == '%' || first == '{') {
			return false
		}
	}
	return true
}

func isOfficePackagePath(lower string) bool {
	// 仅匹配 OOXML 包根（带尾斜杠或精确根名），避免误放行 /pptfoo 等。
	roots := []string{"/ppt/", "/word/", "/xl/", "/docprops/", "/_rels/", "/customxml/"}
	exact := []string{"/ppt", "/word", "/xl", "/docprops", "/_rels", "/customxml"}
	for _, root := range roots {
		if strings.HasPrefix(lower, root) {
			return true
		}
	}
	for _, name := range exact {
		if lower == name {
			return true
		}
	}
	return false
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
