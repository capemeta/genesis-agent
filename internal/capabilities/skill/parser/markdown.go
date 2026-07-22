// Package parser 提供标准 SKILL.md 与 Genesis Runtime Manifest 的严格解析。
package parser

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	"gopkg.in/yaml.v3"
)

const MaxFrontmatterBytes = 64 * 1024

var frontmatterPattern = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---(?:\r?\n|$)`)

type Parser struct{}

func New() *Parser { return &Parser{} }

// frontmatter 只允许可移植 Skill 标准字段。Genesis 控制面字段必须放入 genesis.skill.yaml。
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func (p *Parser) ParseFrontmatter(data []byte, source contract.ParseSource) (model.Metadata, error) {
	head := data
	if len(head) > MaxFrontmatterBytes {
		head = head[:MaxFrontmatterBytes]
	}
	fm, _, err := parseSkillMarkdown(head)
	if err != nil {
		return model.Metadata{}, err
	}
	return metadataFromFrontmatter(fm, source)
}

func (p *Parser) ParseFull(data []byte, source contract.ParseSource) (model.Metadata, string, error) {
	fm, body, err := parseSkillMarkdown(data)
	if err != nil {
		return model.Metadata{}, "", err
	}
	meta, err := metadataFromFrontmatter(fm, source)
	if err != nil {
		return model.Metadata{}, "", err
	}
	return meta, body, nil
}

func parseSkillMarkdown(data []byte) (frontmatter, string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	match := frontmatterPattern.FindSubmatchIndex(data)
	if match == nil {
		return frontmatter{}, "", fmt.Errorf("SKILL.md缺少YAML frontmatter")
	}
	if len(data[match[2]:match[3]]) > MaxFrontmatterBytes {
		return frontmatter{}, "", fmt.Errorf("SKILL.md frontmatter超过%d字节", MaxFrontmatterBytes)
	}
	var fm frontmatter
	if err := decodeStrictYAML(data[match[2]:match[3]], &fm); err != nil {
		return frontmatter{}, "", fmt.Errorf("解析SKILL.md frontmatter失败: %w", err)
	}
	return fm, string(data[match[1]:]), nil
}

func metadataFromFrontmatter(fm frontmatter, source contract.ParseSource) (model.Metadata, error) {
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		return model.Metadata{}, fmt.Errorf("SKILL.md缺少name")
	}
	if err := model.ValidateName(name); err != nil {
		return model.Metadata{}, err
	}
	if source.DirectoryName != "" && source.DirectoryName != name {
		return model.Metadata{}, fmt.Errorf("skill name %q 必须与目录名 %q 一致", name, source.DirectoryName)
	}
	description := strings.TrimSpace(fm.Description)
	if description == "" {
		return model.Metadata{}, fmt.Errorf("skill %q 缺少description", name)
	}
	if len([]rune(description)) > model.MaxDescriptionLen {
		return model.Metadata{}, fmt.Errorf("skill %q description超过%d字符", name, model.MaxDescriptionLen)
	}
	packageID := source.PackageID
	if packageID == "" {
		packageID = model.PackageID(name)
	}
	mainResource := source.MainResource
	if mainResource == "" {
		mainResource = model.ResourceID(string(packageID) + "/SKILL.md")
	}
	return model.Metadata{
		Name: name, Description: description, Scope: source.Scope, Authority: source.Authority,
		PackageID: packageID, MainResource: mainResource, DisplayPath: source.DisplayPath,
		BaseDirectory: source.BaseDirectory, Version: source.Version,
	}.Normalize(), nil
}

func decodeStrictYAML(data []byte, target any) error {
	var root yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&root); err != nil {
		return err
	}
	if err := rejectDuplicateKeys(&root); err != nil {
		return err
	}
	decoder = yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil && len(extra.Content) > 0 {
		return fmt.Errorf("YAML只能包含一个文档")
	}
	return nil
}

func rejectDuplicateKeys(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]struct{}, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := strings.TrimSpace(node.Content[i].Value)
			if _, ok := seen[key]; ok {
				return fmt.Errorf("YAML包含重复字段 %q", key)
			}
			seen[key] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := rejectDuplicateKeys(child); err != nil {
			return err
		}
	}
	return nil
}
