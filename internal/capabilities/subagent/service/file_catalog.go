package service

import (
	"bytes"
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
	MaxDepth        int      `yaml:"max_depth"`
	MaxTokens       int64    `yaml:"max_tokens"`
	MaxToolCalls    int      `yaml:"max_tool_calls"`
	ForkContext     *bool    `yaml:"fork_context"`
	ExecutionMode   string   `yaml:"execution_mode"`
	TimeoutSeconds  int      `yaml:"timeout_seconds"`
}

// LoadProjectDefinitions 只读加载工作区 .genesis/agents 下的 Markdown 定义。
func LoadProjectDefinitions(workspace string) ([]model.Definition, error) {
	dir := filepath.Join(workspace, ".genesis", "agents")
	return loadDefinitionsDirectory(dir)
}

// LoadLocalDefinitions 按 user → 工作区祖先（由远到近）合并本地只读 Definition。
// 同名定义由后加载的更具体目录覆盖，保持与产品无关的可预测优先级。
func LoadLocalDefinitions(workspace, home string) ([]model.Definition, error) {
	workspace = filepath.Clean(workspace)
	directories := make([]string, 0)
	if strings.TrimSpace(home) != "" {
		directories = append(directories, filepath.Join(home, ".genesis", "agents"))
	}
	ancestors := make([]string, 0)
	for current := workspace; ; current = filepath.Dir(current) {
		ancestors = append(ancestors, filepath.Join(current, ".genesis", "agents"))
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	for i := len(ancestors) - 1; i >= 0; i-- {
		directories = append(directories, ancestors[i])
	}
	merged := make(map[string]model.Definition)
	seen := make(map[string]struct{})
	for _, dir := range directories {
		dir = filepath.Clean(dir)
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		definitions, err := loadDefinitionsDirectory(dir)
		if err != nil {
			return nil, err
		}
		for _, definition := range definitions {
			merged[definition.Name] = definition
		}
	}
	definitions := make([]model.Definition, 0, len(merged))
	for _, definition := range merged {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions, nil
}

func loadDefinitionsDirectory(dir string) ([]model.Definition, error) {
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
		path := filepath.Join(dir, entry.Name())
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("读取 Agent 定义 %s 失败: %w", entry.Name(), readErr)
		}
		definition, parseErr := ParseDefinition(string(content))
		if parseErr != nil {
			return nil, fmt.Errorf("Agent 定义 %s 无效: %w", path, parseErr)
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
	decoder := yaml.NewDecoder(bytes.NewBufferString(parts[1]))
	decoder.KnownFields(true)
	if err := decoder.Decode(&meta); err != nil {
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
	if meta.MaxDepth < 0 || meta.MaxDepth > 2 {
		return model.Definition{}, fmt.Errorf("max_depth 必须为 0、1 或 2")
	}
	if meta.MaxTokens < 0 {
		return model.Definition{}, fmt.Errorf("max_tokens 不能小于 0")
	}
	if meta.MaxToolCalls < 0 {
		return model.Definition{}, fmt.Errorf("max_tool_calls 不能小于 0")
	}
	mode := model.ExecutionModeSync
	if raw := strings.TrimSpace(meta.ExecutionMode); raw != "" {
		mode = model.ExecutionMode(raw)
		if mode != model.ExecutionModeSync && mode != model.ExecutionModeAsync {
			return model.Definition{}, fmt.Errorf("execution_mode 必须为 sync 或 async")
		}
	}
	if meta.TimeoutSeconds < 0 {
		return model.Definition{}, fmt.Errorf("timeout_seconds 不能小于 0")
	}
	return model.Definition{Name: meta.Name, Description: meta.Description, WhenToUse: meta.Description, SystemPrompt: body, Tools: tools, MaxTurns: meta.MaxTurns, MaxDepth: meta.MaxDepth, MaxTokens: meta.MaxTokens, MaxToolCalls: meta.MaxToolCalls, ForkContext: meta.ForkContext, ExecutionMode: mode, TimeoutSec: meta.TimeoutSeconds}, nil
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
