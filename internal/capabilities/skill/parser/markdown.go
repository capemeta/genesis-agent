// Package parser 提供 Skill 文件解析。
package parser

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	"gopkg.in/yaml.v3"
)

const MaxFrontmatterBytes = 64 * 1024

var frontmatterPattern = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---(?:\r?\n|$)`)

type Parser struct{}

func New() *Parser { return &Parser{} }

type sandboxSpec struct {
	ExecutionMode    string `yaml:"execution_mode"`
	PreferredBackend string `yaml:"preferred_backend"`
}

type frontmatter struct {
	Name                    string      `yaml:"name"`
	Description             string      `yaml:"description"`
	ShortDescription        string      `yaml:"short-description"`
	Version                 string      `yaml:"version"`
	AllowedTools            []string    `yaml:"allowed-tools"`
	Context                 string      `yaml:"context"`
	Sandbox                 sandboxSpec `yaml:"sandbox"`
	Agent                   string      `yaml:"agent"`
	Model                   string      `yaml:"model"`
	DisableModelInvocation  bool        `yaml:"disable-model-invocation"`
	AllowImplicitInvocation *bool       `yaml:"allow-implicit-invocation"`
	Products                []string    `yaml:"products"`
	MaxThinkingTokens       int         `yaml:"max-thinking-tokens"`
	Dependencies            yaml.Node   `yaml:"dependencies"`
	Requires                yaml.Node   `yaml:"requires"`
	QA                      qaFrontmatter `yaml:"qa"`
}

type qaFrontmatter struct {
	Policy      string `yaml:"policy"`
	Enforcement string `yaml:"enforcement"`
}

func (p *Parser) ParseFrontmatter(data []byte, source contract.ParseSource) (model.Metadata, error) {
	head := data
	if len(head) > MaxFrontmatterBytes {
		head = head[:MaxFrontmatterBytes]
	}
	fm, _, err := parse(head)
	if err != nil {
		return model.Metadata{}, err
	}
	return metadataFromFrontmatter(fm, source)
}

func (p *Parser) ParseFull(data []byte, source contract.ParseSource) (model.Metadata, string, error) {
	fm, body, err := parse(data)
	if err != nil {
		return model.Metadata{}, "", err
	}
	meta, err := metadataFromFrontmatter(fm, source)
	if err != nil {
		return model.Metadata{}, "", err
	}
	return meta, body, nil
}

func parse(data []byte) (frontmatter, string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	match := frontmatterPattern.FindSubmatchIndex(data)
	if match == nil {
		return frontmatter{}, "", fmt.Errorf("SKILL.md缺少YAML frontmatter")
	}
	var fm frontmatter
	if err := yaml.Unmarshal(data[match[2]:match[3]], &fm); err != nil {
		return frontmatter{}, "", fmt.Errorf("解析SKILL.md frontmatter失败: %w", err)
	}
	body := string(data[match[1]:])
	return fm, body, nil
}

func metadataFromFrontmatter(fm frontmatter, source contract.ParseSource) (model.Metadata, error) {
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		name = strings.TrimSpace(source.DirectoryName)
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
	products := make([]profilemodel.ChannelType, 0, len(fm.Products))
	for _, product := range fm.Products {
		product = strings.TrimSpace(strings.ToLower(product))
		if product == "" {
			continue
		}
		products = append(products, profilemodel.ChannelType(product))
	}
	contextMode := model.ContextMode(strings.TrimSpace(strings.ToLower(fm.Context)))
	if contextMode == "" {
		contextMode = model.ContextModeInline
	}
	if contextMode != model.ContextModeInline && contextMode != model.ContextModeFork {
		return model.Metadata{}, fmt.Errorf("skill %q context不支持: %s", name, fm.Context)
	}
	packageID := source.PackageID
	if packageID == "" {
		packageID = model.PackageID(name)
	}
	mainResource := source.MainResource
	if mainResource == "" {
		mainResource = model.ResourceID(string(packageID) + "/SKILL.md")
	}
	meta := model.Metadata{
		Name:             name,
		QualifiedName:    name,
		Description:      description,
		ShortDescription: strings.TrimSpace(fm.ShortDescription),
		Scope:            source.Scope,
		Authority:        source.Authority,
		PackageID:        packageID,
		MainResource:     mainResource,
		DisplayPath:      source.DisplayPath,
		Version:          firstNonEmpty(strings.TrimSpace(fm.Version), source.Version),
		Enabled:          true,
		PromptVisible:    true,
		Policy: model.Policy{
			AllowImplicitInvocation: fm.AllowImplicitInvocation,
			Products:                products,
			DisableModelInvocation:  fm.DisableModelInvocation,
		},
		AllowedTools:      cleanStrings(fm.AllowedTools),
		Context:           contextMode,
		Agent:             strings.TrimSpace(fm.Agent),
		Model:             strings.TrimSpace(fm.Model),
		MaxThinkingTokens: fm.MaxThinkingTokens,
		Dependencies:      cleanDependencies(fm.Dependencies),
		Requires:          cleanRequires(fm.Requires),
		QA: model.QADeclaration{
			Policy:      strings.TrimSpace(fm.QA.Policy),
			Enforcement: strings.TrimSpace(fm.QA.Enforcement),
		},
		SourceRef: map[string]string{
			"base_directory": source.BaseDirectory,
			"display_path":   source.DisplayPath,
		},
	}.Normalize()
	return meta, nil
}

func cleanRequires(node yaml.Node) []model.CapabilityRequirement {
	if node.Kind == 0 || node.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]model.CapabilityRequirement, 0, len(node.Content))
	for _, item := range node.Content {
		req := requirementFromNode(item)
		if strings.TrimSpace(req.Kind) == "" {
			continue
		}
		out = append(out, req)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func requirementFromNode(node *yaml.Node) model.CapabilityRequirement {
	if node == nil {
		return model.CapabilityRequirement{}
	}
	if node.Kind == yaml.ScalarNode {
		return model.CapabilityRequirement{Kind: strings.TrimSpace(node.Value)}
	}
	if node.Kind != yaml.MappingNode {
		return model.CapabilityRequirement{}
	}
	var req model.CapabilityRequirement
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := strings.TrimSpace(node.Content[i].Value)
		val := strings.TrimSpace(node.Content[i+1].Value)
		switch key {
		case "kind":
			req.Kind = val
		case "enforcement":
			req.Enforcement = val
		case "description":
			req.Description = val
		}
	}
	return req
}

func cleanDependencies(node yaml.Node) model.Dependencies {
	deps := model.Dependencies{}
	if node.Kind == 0 {
		return deps
	}
	if node.Kind == yaml.SequenceNode {
		for _, item := range node.Content {
			dep := dependencyFromNode(item)
			if dep.Value != "" {
				deps.Tools = append(deps.Tools, dep)
			}
		}
		return deps
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := strings.TrimSpace(strings.ToLower(node.Content[i].Value))
			value := node.Content[i+1]
			switch key {
			case "tools":
				if value.Kind == yaml.SequenceNode {
					for _, item := range value.Content {
						dep := dependencyFromNode(item)
						if dep.Value != "" {
							deps.Tools = append(deps.Tools, dep)
						}
					}
				}
			case "runtime":
				deps.Runtime = cleanRuntimeDeps(value)
			case "install_hints", "install-hints":
				deps.InstallHints = cleanStringSeq(value)
			}
		}
	}
	return deps
}

func cleanRuntimeDeps(node *yaml.Node) model.RuntimeDeps {
	out := model.RuntimeDeps{}
	if node == nil || node.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := strings.TrimSpace(strings.ToLower(node.Content[i].Value))
		value := node.Content[i+1]
		pkgs := cleanRuntimePackages(value)
		switch key {
		case "python", "pip":
			out.Python = append(out.Python, pkgs...)
		case "node", "npm":
			out.Node = append(out.Node, pkgs...)
		case "system", "bin", "command":
			out.System = append(out.System, pkgs...)
		}
	}
	return out
}

func cleanRuntimePackages(node *yaml.Node) []model.RuntimePackage {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.SequenceNode {
		out := make([]model.RuntimePackage, 0, len(node.Content))
		for _, item := range node.Content {
			if pkg := runtimePackageFromNode(item); pkg.Name != "" {
				out = append(out, pkg)
			}
		}
		return out
	}
	if pkg := runtimePackageFromNode(node); pkg.Name != "" {
		return []model.RuntimePackage{pkg}
	}
	return nil
}

func runtimePackageFromNode(node *yaml.Node) model.RuntimePackage {
	if node == nil {
		return model.RuntimePackage{}
	}
	if node.Kind == yaml.ScalarNode {
		name := strings.TrimSpace(node.Value)
		return model.RuntimePackage{Name: name}
	}
	if node.Kind != yaml.MappingNode {
		return model.RuntimePackage{}
	}
	pkg := model.RuntimePackage{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := strings.TrimSpace(strings.ToLower(node.Content[i].Value))
		value := strings.TrimSpace(node.Content[i+1].Value)
		switch key {
		case "name", "package", "pkg":
			pkg.Name = value
		case "import":
			pkg.Import = value
		case "require", "module":
			pkg.Require = value
		case "command", "bin", "cmd":
			pkg.Command = value
		case "description":
			pkg.Description = value
		}
	}
	return pkg
}

func cleanStringSeq(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		if item == nil {
			continue
		}
		v := strings.TrimSpace(item.Value)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func dependencyFromNode(node *yaml.Node) model.ToolDependency {
	if node == nil {
		return model.ToolDependency{}
	}
	if node.Kind == yaml.ScalarNode {
		value := strings.TrimSpace(node.Value)
		return model.ToolDependency{Type: "tool", Value: value}
	}
	if node.Kind != yaml.MappingNode {
		return model.ToolDependency{}
	}
	dep := model.ToolDependency{Type: "tool"}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := strings.TrimSpace(strings.ToLower(node.Content[i].Value))
		value := strings.TrimSpace(node.Content[i+1].Value)
		switch key {
		case "type", "kind":
			dep.Type = value
		case "value", "name", "tool", "id":
			dep.Value = value
		case "description":
			dep.Description = value
		case "transport":
			dep.Transport = value
		case "command":
			dep.Command = value
		case "url":
			dep.URL = value
		}
	}
	return dep
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
