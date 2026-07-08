package skillmarket

import (
	"encoding/json"
	"fmt"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"os"
	"path/filepath"
	"strings"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
)

const (
	genesisMarketplaceFile = ".genesis/marketplace.json"
	plainMarketplaceFile   = "marketplace.json"
	pluginManifestFile     = "plugin.json"
)

type rawManifest struct {
	Schema      string                `json:"$schema"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Version     string                `json:"version"`
	Owner       marketmodel.Owner     `json:"owner"`
	Packages    []marketmodel.Package `json:"packages"`
	Metadata    map[string]any        `json:"metadata"`
}

type rawPluginManifest struct {
	Schema       string                        `json:"$schema"`
	Name         string                        `json:"name"`
	Description  string                        `json:"description"`
	Version      string                        `json:"version"`
	Owner        marketmodel.Owner             `json:"owner"`
	Capabilities []capmodel.CapabilityManifest `json:"capabilities"`
	Commands     []string                      `json:"commands"`
	Dependencies skillmodel.Dependencies       `json:"dependencies"`
	Products     []string                      `json:"products"`
	Permissions  []capmodel.Permission         `json:"permissions"`
	License      string                        `json:"license"`
	Signature    *capmodel.Signature           `json:"signature"`
	Metadata     map[string]any                `json:"metadata"`
}

func readMarketplaceFromDirectory(root string) (marketmodel.Manifest, error) {
	path, kind, err := marketplaceManifestPath(root)
	if err != nil {
		return marketmodel.Manifest{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return marketmodel.Manifest{}, err
	}
	if kind == pluginManifestFile {
		return readPluginManifest(root, data)
	}
	var raw rawManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return marketmodel.Manifest{}, fmt.Errorf("解析marketplace manifest失败: %w", err)
	}
	manifest := marketmodel.Manifest{Schema: raw.Schema, Name: strings.TrimSpace(raw.Name), Description: strings.TrimSpace(raw.Description), Version: strings.TrimSpace(raw.Version), Owner: raw.Owner, Metadata: raw.Metadata}
	if manifest.Name == "" {
		return marketmodel.Manifest{}, fmt.Errorf("marketplace manifest缺少name")
	}
	if len(raw.Packages) == 0 {
		return marketmodel.Manifest{}, fmt.Errorf("marketplace manifest缺少packages")
	}
	seen := map[string]struct{}{}
	for i, pkg := range raw.Packages {
		converted, err := validatePackage(root, i, pkg)
		if err != nil {
			return marketmodel.Manifest{}, err
		}
		if _, ok := seen[converted.Name]; ok {
			return marketmodel.Manifest{}, fmt.Errorf("重复package name: %s", converted.Name)
		}
		seen[converted.Name] = struct{}{}
		manifest.Packages = append(manifest.Packages, converted)
	}
	return manifest, nil
}

func marketplaceManifestPath(root string) (path string, kind string, err error) {
	for _, rel := range []string{genesisMarketplaceFile, plainMarketplaceFile, pluginManifestFile} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return path, rel, nil
		}
	}
	return "", "", fmt.Errorf("未找到package marketplace manifest，期望 .genesis/marketplace.json、marketplace.json 或 plugin.json")
}

func readPluginManifest(root string, data []byte) (marketmodel.Manifest, error) {
	var raw rawPluginManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return marketmodel.Manifest{}, fmt.Errorf("解析plugin manifest失败: %w", err)
	}
	name := strings.TrimSpace(raw.Name)
	if err := skillmodel.ValidateName(name); err != nil {
		return marketmodel.Manifest{}, fmt.Errorf("plugin.name无效: %w", err)
	}
	pkg := marketmodel.Package{
		Name:         name,
		Type:         marketmodel.PackageTypePlugin,
		Description:  strings.TrimSpace(raw.Description),
		Version:      strings.TrimSpace(raw.Version),
		Source:       "./",
		Capabilities: raw.Capabilities,
		Commands:     raw.Commands,
		Dependencies: raw.Dependencies,
		Products:     raw.Products,
		Permissions:  raw.Permissions,
		License:      strings.TrimSpace(raw.License),
		Signature:    raw.Signature,
		Metadata:     raw.Metadata,
	}
	validated, err := validatePackage(root, 0, pkg)
	if err != nil {
		return marketmodel.Manifest{}, err
	}
	return marketmodel.Manifest{
		Schema:      raw.Schema,
		Name:        name + "-marketplace",
		Description: strings.TrimSpace(raw.Description),
		Version:     strings.TrimSpace(raw.Version),
		Owner:       raw.Owner,
		Packages:    []marketmodel.Package{validated},
		Metadata:    raw.Metadata,
	}, nil
}

func validatePackage(root string, index int, raw marketmodel.Package) (marketmodel.Package, error) {
	name := strings.TrimSpace(raw.Name)
	if err := skillmodel.ValidateName(name); err != nil {
		return marketmodel.Package{}, fmt.Errorf("packages[%d].name无效: %w", index, err)
	}
	pkgType := raw.Type
	if pkgType == "" {
		pkgType = marketmodel.PackageTypeSkillPackage
	}
	if !isSupportedPackageType(pkgType) {
		return marketmodel.Package{}, fmt.Errorf("packages[%d].type暂不支持: %s", index, pkgType)
	}
	source := strings.TrimSpace(raw.Source)
	if source == "" {
		source = "./"
	}
	if err := validateRelativePath(source); err != nil {
		return marketmodel.Package{}, fmt.Errorf("packages[%d].source无效: %w", index, err)
	}
	base, err := safeJoin(root, source)
	if err != nil {
		return marketmodel.Package{}, fmt.Errorf("packages[%d].source越界: %w", index, err)
	}
	if stat, err := os.Stat(base); err != nil || !stat.IsDir() {
		return marketmodel.Package{}, fmt.Errorf("packages[%d].source目录不存在: %s", index, source)
	}
	if len(raw.Capabilities) == 0 {
		return marketmodel.Package{}, fmt.Errorf("packages[%d].capabilities不能为空", index)
	}
	capabilities := make([]capmodel.CapabilityManifest, 0, len(raw.Capabilities))
	seenCapability := map[string]struct{}{}
	for j, capability := range raw.Capabilities {
		converted, err := validateCapability(base, index, j, capability)
		if err != nil {
			return marketmodel.Package{}, err
		}
		key := string(converted.Type) + ":" + converted.Name + ":" + converted.Path
		if _, ok := seenCapability[key]; ok {
			return marketmodel.Package{}, fmt.Errorf("packages[%d].capabilities[%d]重复: %s", index, j, key)
		}
		seenCapability[key] = struct{}{}
		capabilities = append(capabilities, converted)
	}
	for j, rel := range raw.Commands {
		if err := validateRelativePath(rel); err != nil {
			return marketmodel.Package{}, fmt.Errorf("packages[%d].commands[%d]无效: %w", index, j, err)
		}
		if _, err := safeJoin(base, rel); err != nil {
			return marketmodel.Package{}, fmt.Errorf("packages[%d].commands[%d]越界: %w", index, j, err)
		}
	}
	return marketmodel.Package{Name: name, Type: pkgType, Description: strings.TrimSpace(raw.Description), Version: strings.TrimSpace(raw.Version), Source: source, Capabilities: capabilities, Commands: raw.Commands, Dependencies: raw.Dependencies, Products: raw.Products, Permissions: raw.Permissions, License: strings.TrimSpace(raw.License), Signature: raw.Signature, Metadata: raw.Metadata}, nil
}

func isSupportedPackageType(value marketmodel.PackageType) bool {
	switch value {
	case marketmodel.PackageTypeSkillPackage, marketmodel.PackageTypePlugin, marketmodel.PackageTypeToolPackage, marketmodel.PackageTypeMCPPackage, marketmodel.PackageTypeSubAgent:
		return true
	default:
		return false
	}
}
func validateCapability(base string, packageIndex, capabilityIndex int, raw capmodel.CapabilityManifest) (capmodel.CapabilityManifest, error) {
	capability := raw
	capability.Name = strings.TrimSpace(capability.Name)
	if err := skillmodel.ValidateName(capability.Name); err != nil {
		return capmodel.CapabilityManifest{}, fmt.Errorf("packages[%d].capabilities[%d].name无效: %w", packageIndex, capabilityIndex, err)
	}
	if capability.Type == "" {
		return capmodel.CapabilityManifest{}, fmt.Errorf("packages[%d].capabilities[%d].type不能为空", packageIndex, capabilityIndex)
	}
	if capability.Path == "" {
		capability.Path = "./"
	}
	if err := validateRelativePath(capability.Path); err != nil {
		return capmodel.CapabilityManifest{}, fmt.Errorf("packages[%d].capabilities[%d].path无效: %w", packageIndex, capabilityIndex, err)
	}
	path, err := safeJoin(base, capability.Path)
	if err != nil {
		return capmodel.CapabilityManifest{}, fmt.Errorf("packages[%d].capabilities[%d].path越界: %w", packageIndex, capabilityIndex, err)
	}
	switch capability.Type {
	case capmodel.CapabilityTypeSkill:
		if err := validateSkillDir(path); err != nil {
			return capmodel.CapabilityManifest{}, fmt.Errorf("packages[%d].capabilities[%d]无效: %w", packageIndex, capabilityIndex, err)
		}
	case capmodel.CapabilityTypeTool, capmodel.CapabilityTypeMCP, capmodel.CapabilityTypeSubAgent, capmodel.CapabilityTypeResource:
		if _, err := os.Stat(path); err != nil {
			return capmodel.CapabilityManifest{}, fmt.Errorf("packages[%d].capabilities[%d].path不存在: %w", packageIndex, capabilityIndex, err)
		}
	default:
		return capmodel.CapabilityManifest{}, fmt.Errorf("packages[%d].capabilities[%d].type暂不支持: %s", packageIndex, capabilityIndex, capability.Type)
	}
	return capability, nil
}

func validateSkillDir(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !stat.IsDir() {
		return fmt.Errorf("skill路径不是目录: %s", path)
	}
	name := filepath.Base(path)
	for _, file := range []string{"SKILL.md", "skill.md"} {
		data, err := os.ReadFile(filepath.Join(path, file))
		if err != nil {
			continue
		}
		if len(data) > skillparser.MaxFrontmatterBytes {
			data = data[:skillparser.MaxFrontmatterBytes]
		}
		_, err = skillparser.New().ParseFrontmatter(data, skillcontract.ParseSource{DirectoryName: name})
		return err
	}
	return fmt.Errorf("缺少SKILL.md: %s", path)
}

func validateRelativePath(rel string) error {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return fmt.Errorf("路径不能为空")
	}
	if strings.Contains(rel, "\\") {
		return fmt.Errorf("必须使用正斜杠相对路径")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return fmt.Errorf("禁止绝对路径")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if clean == "." {
		return nil
	}
	if strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(clean, "/../") {
		return fmt.Errorf("禁止..路径")
	}
	if !strings.HasPrefix(rel, "./") {
		return fmt.Errorf("路径必须以./开头")
	}
	return nil
}

func safeJoin(root, rel string) (string, error) {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanRoot = filepath.Clean(cleanRoot)
	joined := filepath.Join(cleanRoot, filepath.FromSlash(rel))
	joined = filepath.Clean(joined)
	if !isWithin(cleanRoot, joined) {
		return "", fmt.Errorf("path escapes root")
	}
	return joined, nil
}

func isWithin(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(rootAbs), filepath.Clean(pathAbs))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}
