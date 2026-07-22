package parser

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"genesis-agent/internal/capabilities/skill/contract"
	skilleval "genesis-agent/internal/capabilities/skill/eval"
	"genesis-agent/internal/capabilities/skill/model"
	"gopkg.in/yaml.v3"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Finding struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Path     string   `json:"path,omitempty"`
	Message  string   `json:"message"`
}

type ValidationResult struct {
	Metadata model.Metadata `json:"metadata,omitempty"`
	Findings []Finding      `json:"findings,omitempty"`
}

func (r ValidationResult) HasErrors() bool {
	for _, finding := range r.Findings {
		if finding.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (r *ValidationResult) add(severity Severity, code, resource, message string) {
	r.Findings = append(r.Findings, Finding{Severity: severity, Code: code, Path: resource, Message: message})
}

type Validator struct {
	parser *Parser
}

func NewValidator() *Validator {
	return &Validator{parser: New()}
}

func (v *Validator) ValidateSkillFS(root fs.FS, source contract.ParseSource) ValidationResult {
	result := ValidationResult{}
	skillPath, data, ok := readSkillDocument(root, &result)
	if !ok {
		return result
	}
	if !utf8.Valid(data) {
		result.add(SeverityError, "skill_md_not_utf8", skillPath, "SKILL.md必须是UTF-8文本")
		return result
	}

	meta, body, err := v.parser.ParseFull(data, source)
	if err != nil {
		result.add(SeverityError, "frontmatter_invalid", skillPath, err.Error())
	} else {
		result.Metadata = meta
		validateMetadata(meta, skillPath, &result)
	}

	validateFrontmatterShape(data, skillPath, &result)
	validateResourceReferences(root, string(data), &result)
	validateDirectories(root, &result)
	validateEvals(root, result.Metadata.Name, &result)
	validateRiskSignals(root, skillPath, string(data), &result)

	if strings.TrimSpace(body) == "" {
		result.add(SeverityWarning, "body_empty", skillPath, "SKILL.md正文为空，Skill触发后缺少执行说明")
	}
	sortFindings(result.Findings)
	return result
}

func readSkillDocument(root fs.FS, result *ValidationResult) (string, []byte, bool) {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		data, err := fs.ReadFile(root, name)
		if err == nil {
			if name == "skill.md" {
				result.add(SeverityWarning, "skill_md_case", name, "建议使用标准文件名SKILL.md")
			}
			return name, data, true
		}
	}
	result.add(SeverityError, "skill_md_missing", "SKILL.md", "缺少SKILL.md")
	return "", nil, false
}

func validateMetadata(meta model.Metadata, skillPath string, result *ValidationResult) {
	if strings.ContainsAny(meta.Description, "<>") {
		result.add(SeverityError, "description_angle_brackets", skillPath, "description不能包含尖括号")
	}
	if len([]rune(meta.Description)) > model.MaxDescriptionLen {
		result.add(SeverityError, "description_too_long", skillPath, fmt.Sprintf("description不能超过%d字符", model.MaxDescriptionLen))
	}
	for _, tool := range meta.AllowedTools {
		if strings.TrimSpace(tool) == "" {
			result.add(SeverityWarning, "allowed_tool_empty", skillPath, "allowed-tools包含空值")
		}
	}
	for _, dep := range meta.Dependencies.Tools {
		if dep.Type == "" || dep.Value == "" {
			result.add(SeverityWarning, "dependency_incomplete", skillPath, "dependencies.tools中存在缺少type或value的依赖")
		}
	}
}

func validateFrontmatterShape(data []byte, skillPath string, result *ValidationResult) {
	match := frontmatterPattern.FindSubmatch(data)
	if match == nil {
		return
	}
	var node yaml.Node
	if err := yaml.Unmarshal(match[1], &node); err != nil {
		return
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		result.add(SeverityError, "frontmatter_not_mapping", skillPath, "frontmatter必须是YAML对象")
		return
	}
	allowed := map[string]struct{}{
		"name": {}, "description": {}, "short-description": {}, "version": {},
		"allowed-tools": {}, "context": {}, "agent": {}, "model": {},
		"disable-model-invocation": {}, "allow-implicit-invocation": {},
		"products": {}, "max-thinking-tokens": {}, "dependencies": {},
		"requires": {}, "qa": {}, "sandbox": {},
		"metadata": {}, "license": {}, "compatibility": {},
	}
	mapping := node.Content[0]
	seenName := false
	seenDescription := false
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := strings.TrimSpace(mapping.Content[i].Value)
		value := mapping.Content[i+1]
		switch key {
		case "name":
			seenName = true
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				result.add(SeverityError, "name_not_string", skillPath, "name必须是字符串")
			}
		case "description":
			seenDescription = true
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				result.add(SeverityError, "description_not_string", skillPath, "description必须是字符串")
			}
		case "allowed-tools", "products", "requires":
			if value.Kind != yaml.SequenceNode {
				result.add(SeverityError, key+"_not_sequence", skillPath, key+"必须是字符串数组或对象数组")
			}
		case "dependencies", "metadata", "qa", "sandbox":
			if value.Kind != yaml.MappingNode && value.Kind != yaml.SequenceNode {
				result.add(SeverityWarning, key+"_shape", skillPath, key+"建议使用YAML对象或数组")
			}
		}
		if _, ok := allowed[key]; !ok {
			result.add(SeverityInfo, "frontmatter_extension", skillPath, "未识别frontmatter字段"+key+"，将按外部扩展处理")
		}
	}
	if !seenName {
		result.add(SeverityError, "name_missing", skillPath, "缺少name")
	}
	if !seenDescription {
		result.add(SeverityError, "description_missing", skillPath, "缺少description")
	}
}

var markdownResourcePattern = regexp.MustCompile(`\]\(([^)]+)\)|` + "`" + `((?:references|scripts|assets)/[^` + "`" + `]+)` + "`")
var fencedCodePattern = regexp.MustCompile("(?s)```.*?```")

func validateResourceReferences(root fs.FS, content string, result *ValidationResult) {
	content = stripFencedCodeBlocks(content)
	seen := map[string]struct{}{}
	for _, match := range markdownResourcePattern.FindAllStringSubmatch(content, -1) {
		ref := strings.TrimSpace(firstNonEmpty(match[1], match[2]))
		ref = normalizeResourceRef(ref)
		if ref == "" || !isSkillResourcePath(ref) {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		if _, err := fs.Stat(root, ref); err != nil {
			result.add(SeverityWarning, "resource_missing", ref, "SKILL.md引用的资源不存在")
		}
	}
}

func stripFencedCodeBlocks(content string) string {
	return fencedCodePattern.ReplaceAllString(content, "")
}
func normalizeResourceRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.Contains(ref, "://") || strings.HasPrefix(ref, "#") {
		return ""
	}
	ref = strings.TrimPrefix(ref, "./")
	if idx := strings.Index(ref, "#"); idx >= 0 {
		ref = ref[:idx]
	}
	if idx := strings.Index(ref, "?"); idx >= 0 {
		ref = ref[:idx]
	}
	ref = path.Clean(strings.ReplaceAll(ref, "\\", "/"))
	if ref == "." || strings.HasPrefix(ref, "../") || strings.Contains(ref, "/../") {
		return ""
	}
	return ref
}

func isSkillResourcePath(ref string) bool {
	return strings.HasPrefix(ref, "references/") || strings.HasPrefix(ref, "scripts/") || strings.HasPrefix(ref, "assets/")
}

func validateDirectories(root fs.FS, result *ValidationResult) {
	for _, dir := range []string{"references", "scripts", "assets", "evals"} {
		entries, err := fs.ReadDir(root, dir)
		if err != nil {
			continue
		}
		if len(entries) == 0 {
			result.add(SeverityInfo, "empty_optional_dir", dir, "可选目录为空，确认是否需要保留")
		}
	}
}

func validateEvals(root fs.FS, skillName string, result *ValidationResult) {
	evalResult := skilleval.NewValidator().ValidateFS(root, skillName)
	for _, finding := range evalResult.Findings {
		severity := SeverityInfo
		switch finding.Severity {
		case skilleval.SeverityError:
			severity = SeverityError
		case skilleval.SeverityWarning:
			severity = SeverityWarning
		case skilleval.SeverityInfo:
			severity = SeverityInfo
		}
		result.add(severity, finding.Code, finding.Path, finding.Message)
	}
}

var (
	windowsAbsPathPattern = regexp.MustCompile(`[A-Za-z]:\\[^\s` + "`" + `"'<>]+`)
	unixAbsPathPattern    = regexp.MustCompile(`(?m)(^|\s)/(Users|home|workspace|tmp)/[^\s` + "`" + `"'<>]+`)
	tokenPattern          = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|cookie)\s*[:=]\s*['"]?[A-Za-z0-9_\-./+=]{16,}`)
)

func validateRiskSignals(root fs.FS, skillPath, content string, result *ValidationResult) {
	scanText(skillPath, content, result)
	_ = fs.WalkDir(root, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if p == skillPath {
			return nil
		}
		if !strings.HasPrefix(p, "scripts/") && !strings.HasPrefix(p, "references/") {
			return nil
		}
		if strings.HasPrefix(p, "scripts/") {
			result.add(SeverityWarning, "script_review_required", p, "scripts目录中的文件需要声明命令、文件写入、网络和依赖风险")
		}
		data, readErr := fs.ReadFile(root, p)
		if readErr == nil && utf8.Valid(data) {
			scanText(p, string(data), result)
		}
		return nil
	})
}

func scanText(resource, content string, result *ValidationResult) {
	if strings.Contains(content, "BEGIN PRIVATE KEY") {
		result.add(SeverityError, "private_key_detected", resource, "发现疑似私钥内容，不能打包到Skill")
	}
	if tokenPattern.FindString(content) != "" {
		result.add(SeverityWarning, "secret_like_value", resource, "发现疑似token/secret/cookie，请确认不要把真实密钥写入Skill")
	}
	if windowsAbsPathPattern.FindString(content) != "" || unixAbsPathPattern.FindString(content) != "" {
		result.add(SeverityWarning, "absolute_path_detected", resource, "发现疑似宿主机绝对路径，可分发Skill应使用相对路径或resource id")
	}
}

func sortFindings(findings []Finding) {
	order := map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}
	sort.SliceStable(findings, func(i, j int) bool {
		if order[findings[i].Severity] != order[findings[j].Severity] {
			return order[findings[i].Severity] < order[findings[j].Severity]
		}
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Code < findings[j].Code
	})
}
