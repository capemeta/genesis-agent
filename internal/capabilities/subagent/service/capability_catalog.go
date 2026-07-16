package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	capcontract "genesis-agent/internal/capabilities/capability/contract"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"genesis-agent/internal/capabilities/subagent/model"
)

// LoadCapabilityDefinitions 从已安装的 subagent capability 读取 Definition。
// 该 Source 只信任 Registry 已验证的安装根目录，并拒绝越出安装目录的资源路径。
func LoadCapabilityDefinitions(ctx context.Context, registry capcontract.Registry, product string) ([]model.Definition, error) {
	if registry == nil {
		return nil, nil
	}
	records, err := registry.ListCapabilities(ctx, capmodel.CapabilityQuery{
		Types:   []capmodel.CapabilityType{capmodel.CapabilityTypeSubAgent},
		Product: strings.TrimSpace(product),
	})
	if err != nil {
		return nil, fmt.Errorf("查询 subagent capability: %w", err)
	}
	definitions := make([]model.Definition, 0, len(records))
	for _, record := range records {
		definition, err := definitionFromCapability(record)
		if err != nil {
			return nil, fmt.Errorf("加载 subagent capability %q: %w", record.ID, err)
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions, nil
}

func definitionFromCapability(record capmodel.CapabilityIndexRecord) (model.Definition, error) {
	if record.Type != capmodel.CapabilityTypeSubAgent || !record.Enabled {
		return model.Definition{}, fmt.Errorf("capability 必须是已启用的 subagent")
	}
	installRoot := strings.TrimSpace(record.InstallRoot)
	resourcePath := strings.TrimSpace(record.ResourcePath)
	if installRoot == "" || resourcePath == "" {
		return model.Definition{}, fmt.Errorf("必须提供 install_root 和 resource_path")
	}
	if filepath.IsAbs(resourcePath) {
		return model.Definition{}, fmt.Errorf("resource_path 必须是相对安装目录的路径")
	}
	root, err := filepath.Abs(installRoot)
	if err != nil {
		return model.Definition{}, fmt.Errorf("解析 install_root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return model.Definition{}, fmt.Errorf("解析 install_root 符号链接: %w", err)
	}
	path, err := filepath.Abs(filepath.Join(root, resourcePath))
	if err != nil {
		return model.Definition{}, fmt.Errorf("解析 resource_path: %w", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return model.Definition{}, fmt.Errorf("解析 resource_path 符号链接: %w", err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return model.Definition{}, fmt.Errorf("resource_path 越出 install_root")
	}
	if !strings.EqualFold(filepath.Ext(path), ".md") {
		return model.Definition{}, fmt.Errorf("resource_path 必须指向 .md 定义文件")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return model.Definition{}, fmt.Errorf("读取定义文件: %w", err)
	}
	definition, err := ParseDefinition(string(content))
	if err != nil {
		return model.Definition{}, err
	}
	return definition, nil
}
