// Package card 提供 Skill 发布卡片的生成和校验。
package card

import (
	"bytes"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"genesis-agent/internal/capabilities/skill/model"
)

const (
	// SkillCardPath 是 Skill 包内发布治理卡片的标准路径。
	SkillCardPath = "skill-card.md"
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
	Found    bool      `json:"found"`
	Findings []Finding `json:"findings,omitempty"`
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

type Validator struct{}

func NewValidator() *Validator { return &Validator{} }

type Section struct {
	Title       string
	Description string
}

var RequiredSections = []Section{
	{"Description", "Skill 做什么，以及不会做什么。"},
	{"Owner", "维护人或维护团队。"},
	{"License And Terms", "许可证、使用条款和再分发限制。"},
	{"Use Case", "适用场景、目标用户和非目标场景。"},
	{"Deployment Geography", "预期部署地区、数据出境或地域限制。"},
	{"Requirements And Dependencies", "依赖的工具、MCP、命令、网络、文件权限或外部服务。"},
	{"Known Risks And Mitigations", "已知风险、误用场景和缓解方式。"},
	{"References", "规范、文档、数据源或上游来源。"},
	{"Skill Output", "Skill 预期产物、格式和成功标准。"},
	{"Skill Version", "版本和兼容性信息。"},
	{"Ethical Considerations", "公平性、安全性、隐私、合规或人工复核要求。"},
}

var headingPattern = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`)

func (v *Validator) ValidateFS(root fs.FS) ValidationResult {
	result := ValidationResult{}
	data, err := fs.ReadFile(root, SkillCardPath)
	if err != nil {
		result.add(SeverityWarning, "skill_card_missing", SkillCardPath, "缺少skill-card.md；个人草稿可忽略，团队/marketplace/企业发布建议补充")
		return result
	}
	result.Found = true
	content := string(data)
	headings := collectHeadings(content)
	for _, section := range RequiredSections {
		key := normalizeHeading(section.Title)
		if _, ok := headings[key]; !ok {
			result.add(SeverityWarning, "skill_card_section_missing", SkillCardPath, "缺少章节: "+section.Title)
		}
	}
	if strings.Contains(content, "TODO") || strings.Contains(content, "TBD") {
		result.add(SeverityInfo, "skill_card_placeholder", SkillCardPath, "skill-card.md仍包含TODO/TBD占位内容")
	}
	sort.SliceStable(result.Findings, func(i, j int) bool {
		order := map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}
		if order[result.Findings[i].Severity] != order[result.Findings[j].Severity] {
			return order[result.Findings[i].Severity] < order[result.Findings[j].Severity]
		}
		return result.Findings[i].Code < result.Findings[j].Code
	})
	return result
}

func collectHeadings(content string) map[string]struct{} {
	headings := map[string]struct{}{}
	for _, match := range headingPattern.FindAllStringSubmatch(content, -1) {
		headings[normalizeHeading(match[1])] = struct{}{}
	}
	return headings
}

func normalizeHeading(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "&", "and")
	value = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

type TemplateData struct {
	SkillName        string
	Description      string
	Owner            string
	License          string
	Version          string
	DependencyHint   string
	ReferenceHint    string
	OutputHint       string
	DeploymentRegion string
}

func TemplateDataFromMetadata(meta model.Metadata, owner, license, version string) TemplateData {
	if strings.TrimSpace(owner) == "" {
		owner = "TODO"
	}
	if strings.TrimSpace(license) == "" {
		license = "TODO"
	}
	if strings.TrimSpace(version) == "" {
		version = firstNonEmpty(meta.Version, "0.1.0")
	}
	dependencyHint := "None declared."
	if len(meta.AllowedTools) > 0 {
		dependencyHint = "Allowed tools: " + strings.Join(meta.AllowedTools, ", ") + "."
	}
	return TemplateData{
		SkillName:        meta.Name,
		Description:      meta.Description,
		Owner:            owner,
		License:          license,
		Version:          version,
		DependencyHint:   dependencyHint,
		ReferenceHint:    "Add upstream documentation, source repositories, datasets, or internal policy references.",
		OutputHint:       "Describe the expected artifact, answer format, or operational result.",
		DeploymentRegion: "Local/user workspace by default; update before team or enterprise release.",
	}
}

func Render(data TemplateData) (string, error) {
	tpl, err := template.New("skill-card").Parse(skillCardTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("渲染skill-card.md失败: %w", err)
	}
	return buf.String(), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

const skillCardTemplate = `# {{ .SkillName }} Skill Card

## Description

{{ .Description }}

## Owner

{{ .Owner }}

## License And Terms

{{ .License }}

## Use Case

Describe when this skill should be used, who it is for, and where it should not be used.

## Deployment Geography

{{ .DeploymentRegion }}

## Requirements And Dependencies

{{ .DependencyHint }}

## Known Risks And Mitigations

List material risks such as unsafe commands, external data transfer, hallucination-sensitive output, privacy concerns, or required human review. Add mitigations for each risk.

## References

{{ .ReferenceHint }}

## Skill Output

{{ .OutputHint }}

## Skill Version

{{ .Version }}

## Ethical Considerations

Document privacy, compliance, safety, fairness, and human oversight considerations before publishing beyond personal use.
`
