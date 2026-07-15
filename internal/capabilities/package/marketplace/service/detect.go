package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	capmodel "genesis-agent/internal/capabilities/capability/model"
	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillparser "genesis-agent/internal/capabilities/skill/parser"
)

type detectResult struct {
	Manifest    marketmodel.Manifest
	Packages    []marketmodel.Package
	NeedsChoice bool
	Candidates  []string
	Message     string
	FailureKind string
}

// detectInstallTargets 根据 fetch 落盘内容与请求选择要安装的 skill-package。
func detectInstallTargets(root string, fetchedManifest marketmodel.Manifest, source marketmodel.MarketplaceSource, packageName, skillPath string, skillPaths []string) detectResult {
	if len(skillPaths) > 0 {
		pkgs := make([]marketmodel.Package, 0, len(skillPaths))
		for _, p := range skillPaths {
			pkg, err := synthesizeSkillPackage(root, p, source)
			if err != nil {
				return detectResult{FailureKind: "validation_failed", Message: err.Error()}
			}
			pkgs = append(pkgs, pkg)
		}
		return detectResult{Manifest: syntheticManifest(source, pkgs), Packages: pkgs}
	}
	if skillPath != "" {
		pkg, err := synthesizeSkillPackage(root, skillPath, source)
		if err != nil {
			return detectResult{FailureKind: "validation_failed", Message: err.Error()}
		}
		pkgs := []marketmodel.Package{pkg}
		return detectResult{Manifest: syntheticManifest(source, pkgs), Packages: pkgs}
	}

	if len(fetchedManifest.Packages) > 0 {
		return detectFromManifest(fetchedManifest, packageName)
	}

	// 无 manifest：单 Skill 或多 Skill 扫描
	if hasSkillMD(root) {
		pkg, err := synthesizeSkillPackage(root, ".", source)
		if err != nil {
			return detectResult{FailureKind: "validation_failed", Message: err.Error()}
		}
		pkgs := []marketmodel.Package{pkg}
		return detectResult{Manifest: syntheticManifest(source, pkgs), Packages: pkgs}
	}
	candidates, err := listSkillSubdirs(root)
	if err != nil {
		return detectResult{FailureKind: "validation_failed", Message: err.Error()}
	}
	if len(candidates) == 0 {
		return detectResult{FailureKind: "validation_failed", Message: "来源中未找到 SKILL.md 或 marketplace manifest"}
	}
	if len(candidates) == 1 {
		pkg, err := synthesizeSkillPackage(root, candidates[0], source)
		if err != nil {
			return detectResult{FailureKind: "validation_failed", Message: err.Error()}
		}
		pkgs := []marketmodel.Package{pkg}
		return detectResult{Manifest: syntheticManifest(source, pkgs), Packages: pkgs}
	}
	return detectResult{
		NeedsChoice: true,
		Candidates:  candidates,
		FailureKind: "needs_choice",
		Message:     "发现多个 Skill，请通过 skill_path 指定",
	}
}

func detectFromManifest(manifest marketmodel.Manifest, packageName string) detectResult {
	var skillPkgs []marketmodel.Package
	var pluginOnly bool
	for _, pkg := range manifest.Packages {
		switch pkg.Type {
		case marketmodel.PackageTypePlugin:
			pluginOnly = true
		case marketmodel.PackageTypeSkillPackage, "":
			skillPkgs = append(skillPkgs, pkg)
		}
	}
	if len(skillPkgs) == 0 {
		if pluginOnly {
			return detectResult{
				FailureKind: "validation_failed",
				Message:     "来源是 plugin 包，请使用 plugin install，而不是 skill install",
			}
		}
		return detectResult{FailureKind: "validation_failed", Message: "marketplace 中没有 skill-package"}
	}
	if packageName != "" {
		for _, pkg := range skillPkgs {
			if pkg.Name == packageName {
				return detectResult{Manifest: manifest, Packages: []marketmodel.Package{pkg}}
			}
		}
		return detectResult{FailureKind: "validation_failed", Message: fmt.Sprintf("package %q 不存在", packageName)}
	}
	if len(skillPkgs) == 1 {
		return detectResult{Manifest: manifest, Packages: []marketmodel.Package{skillPkgs[0]}}
	}
	candidates := make([]string, 0, len(skillPkgs))
	for _, pkg := range skillPkgs {
		candidates = append(candidates, marketmodel.PackageSpec(pkg.Name, manifest.Name))
	}
	return detectResult{
		NeedsChoice: true,
		Candidates:  candidates,
		FailureKind: "needs_choice",
		Message:     "marketplace 含多个 skill-package，请指定 package",
	}
}

func synthesizeSkillPackage(root, rel string, source marketmodel.MarketplaceSource) (marketmodel.Package, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		rel = "./"
	}
	if strings.Contains(rel, "..") {
		return marketmodel.Package{}, fmt.Errorf("skill_path不能包含..")
	}
	dir := root
	if rel != "./" {
		clean := strings.TrimPrefix(filepath.ToSlash(rel), "./")
		dir = filepath.Join(root, filepath.FromSlash(clean))
	}
	name, err := readSkillName(dir)
	if err != nil {
		return marketmodel.Package{}, err
	}
	path := "./"
	if rel != "./" {
		path = "./" + strings.TrimPrefix(filepath.ToSlash(rel), "./")
	}
	_ = source
	return marketmodel.Package{
		Name: name,
		Type: marketmodel.PackageTypeSkillPackage,
		Source: "./",
		Capabilities: []capmodel.CapabilityManifest{{
			Type: capmodel.CapabilityTypeSkill,
			Name: name,
			Path: path,
		}},
	}, nil
}

func syntheticManifest(source marketmodel.MarketplaceSource, pkgs []marketmodel.Package) marketmodel.Manifest {
	return marketmodel.Manifest{
		Name:     syntheticMarketplaceName(source),
		Packages: pkgs,
	}
}

func syntheticMarketplaceName(source marketmodel.MarketplaceSource) string {
	switch source.Type {
	case marketmodel.SourceTypeGitHub:
		return "github-" + strings.ReplaceAll(source.Repo, "/", "-")
	case marketmodel.SourceTypeURL:
		return "url-source"
	case marketmodel.SourceTypeDirectory, marketmodel.SourceTypeFile:
		return "local-source"
	default:
		return "remote-source"
	}
}

func hasSkillMD(dir string) bool {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

func listSkillSubdirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		sub := filepath.Join(root, entry.Name())
		if hasSkillMD(sub) {
			out = append(out, entry.Name())
		}
	}
	return out, nil
}

func readSkillName(dir string) (string, error) {
	for _, file := range []string{"SKILL.md", "skill.md"} {
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			continue
		}
		if len(data) > skillparser.MaxFrontmatterBytes {
			data = data[:skillparser.MaxFrontmatterBytes]
		}
		// DirectoryName 留空：fetch cache 目录名常与 skill name 不一致；最终校验仍要求合法 name。
		// 安装投影时 capability 目录名由 Installer 落盘路径决定。
		meta, err := skillparser.New().ParseFrontmatter(data, skillcontract.ParseSource{})
		if err != nil {
			return "", err
		}
		name := strings.TrimSpace(meta.Name)
		if name == "" {
			name = filepath.Base(dir)
		}
		if err := skillmodel.ValidateName(name); err != nil {
			return "", err
		}
		return name, nil
	}
	return "", fmt.Errorf("缺少SKILL.md: %s", dir)
}
