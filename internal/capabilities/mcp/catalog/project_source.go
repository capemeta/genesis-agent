package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
	platformconfig "genesis-agent/internal/platform/config"

	"gopkg.in/yaml.v3"
)

const precedenceProject = 40

// ProjectSource 从项目级 `.genesis/mcp(.yaml|.yml|.json)` 读取 server 定义。
// Origin=project，连接前需预连接审批（对齐 Kode mcpServerApproval）。
type ProjectSource struct {
	Workspace string
}

// NewProjectSource 创建项目来源；workspace 为空则 List 返回空。
func NewProjectSource(workspace string) *ProjectSource {
	return &ProjectSource{Workspace: strings.TrimSpace(workspace)}
}

func (s *ProjectSource) Precedence() int { return precedenceProject }

func (s *ProjectSource) List(ctx context.Context, env contract.RuntimeCatalogEnv) ([]model.McpServerDefinition, error) {
	_ = env
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.Workspace == "" {
		return nil, nil
	}
	path, raw, err := readProjectMCPFile(s.Workspace)
	if err != nil {
		return nil, err
	}
	if path == "" || len(raw) == 0 {
		return nil, nil
	}
	servers, err := parseProjectServers(raw, path)
	if err != nil {
		return nil, err
	}
	out := make([]model.McpServerDefinition, 0, len(servers))
	for name, dto := range servers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		cfg, err := MapServerDTO(name, dto, platformconfig.MCPConfig{})
		if err != nil {
			return nil, fmt.Errorf("project mcp %s: %w", path, err)
		}
		def := model.McpServerDefinition{Config: cfg, Origin: model.OriginProject}
		def.ConfigKey = ComputeConfigKey(def)
		out = append(out, def)
	}
	return out, nil
}

type projectFile struct {
	Servers map[string]platformconfig.MCPServerConfig `json:"servers" yaml:"servers"`
}

func readProjectMCPFile(workspace string) (string, []byte, error) {
	candidates := []string{
		filepath.Join(workspace, ".genesis", "mcp.yaml"),
		filepath.Join(workspace, ".genesis", "mcp.yml"),
		filepath.Join(workspace, ".genesis", "mcp.json"),
		filepath.Join(workspace, ".genesis", "mcp"),
	}
	for _, p := range candidates {
		raw, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", nil, fmt.Errorf("读取项目 MCP 配置失败: %w", err)
		}
		return p, raw, nil
	}
	return "", nil, nil
}

func parseProjectServers(raw []byte, path string) (map[string]platformconfig.MCPServerConfig, error) {
	var file projectFile
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
		}
	default:
		// .yaml/.yml/无扩展名：按 YAML 解析
		if err := yaml.Unmarshal(raw, &file); err != nil {
			// 兼容仅含 servers map 的顶层结构失败后，再试 JSON
			if err2 := json.Unmarshal(raw, &file); err2 != nil {
				return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
			}
		}
	}
	if file.Servers == nil {
		return map[string]platformconfig.MCPServerConfig{}, nil
	}
	return file.Servers, nil
}

var _ contract.DefinitionSource = (*ProjectSource)(nil)
