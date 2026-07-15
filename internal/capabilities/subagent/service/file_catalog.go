package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"genesis-agent/internal/capabilities/subagent/model"
)

var definitionName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type frontmatter struct {
	Name            string   `yaml:"name"`
	Description     string   `yaml:"description"`
	Tools           []string `yaml:"tools"`
	DisallowedTools []string `yaml:"disallowed_tools"`
	MaxTurns        int      `yaml:"max_turns"`
}

// LoadProjectDefinitions 只读加载工作区 .genesis/agents 下的 Markdown 定义。
func LoadProjectDefinitions(workspace string) ([]model.Definition, error) {
	dir := filepath.Join(workspace, ".genesis", "agents")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取项目 agents 目录失败: %w", err)
	}
	definitions := make([]model.Definition, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			return nil, fmt.Errorf("读取 Agent 定义 %s 失败: %w", entry.Name(), readErr)
		}
		definition, parseErr := ParseDefinition(string(content))
		if parseErr != nil {
			return nil, fmt.Errorf("Agent 定义 %s 无效: %w", entry.Name(), parseErr)
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions, nil
}

// ParseDefinition 校验 Markdown frontmatter，禁止定义绕过编排工具限制。
func ParseDefinition(content string) (model.Definition, error) {
	parts := strings.SplitN(content, "---", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[0]) != "" {
		return model.Definition{}, fmt.Errorf("frontmatter 必须以 --- 包裹")
	}
	var meta frontmatter
	if err := yaml.Unmarshal([]byte(parts[1]), &meta); err != nil {
		return model.Definition{}, fmt.Errorf("解析 frontmatter 失败: %w", err)
	}
	meta.Name, meta.Description = strings.TrimSpace(meta.Name), strings.TrimSpace(meta.Description)
	if !definitionName.MatchString(meta.Name) {
		return model.Definition{}, fmt.Errorf("name 必须为字母数字、- 或 _")
	}
	if meta.Description == "" {
		return model.Definition{}, fmt.Errorf("description 不能为空")
	}
	body := strings.TrimSpace(parts[2])
	if body == "" {
		return model.Definition{}, fmt.Errorf("正文不能为空")
	}
	tools := make([]string, 0, len(meta.Tools))
	denied := toSet(meta.DisallowedTools)
	for _, name := range meta.Tools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if name == "Task" || name == "TaskOutput" || name == "TaskStop" || denied[name] {
			continue
		}
		tools = append(tools, name)
	}
	if meta.MaxTurns < 0 {
		return model.Definition{}, fmt.Errorf("max_turns 不能小于 0")
	}
	return model.Definition{Name: meta.Name, Description: meta.Description, WhenToUse: meta.Description, SystemPrompt: body, Tools: tools, MaxTurns: meta.MaxTurns}, nil
}
func toSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[strings.TrimSpace(value)] = true
	}
	return out
}

// NewMergedCatalog 以后者覆盖同名定义，保持内置与项目定义统一入口。
func NewMergedCatalog(sources ...[]model.Definition) *MemoryCatalog {
	var definitions []model.Definition
	for _, source := range sources {
		definitions = append(definitions, source...)
	}
	return NewMemoryCatalog(definitions)
}
