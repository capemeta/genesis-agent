package materialize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

// Result 是脚本树落盘结果。
type Result struct {
	SkillDir     string
	ScriptsDir   string
	Files        []string // scripts/ 下的相对文件，保留给入口脚本调用方使用。
	PackageFiles []string // skillDir 下的完整包相对文件。
}

// Materializer 将 Skill scripts/ 落到可执行目录。
type Materializer struct {
	Service skillcontract.Service
}

// MaterializePackageScripts 把 Skill 包资源写到 skillDir；其中 scripts/ 文件同时记录到 Files。
func (m *Materializer) MaterializePackageScripts(ctx context.Context, catalog skillcontract.CatalogRequest, meta model.Metadata, skillDir string) (*Result, error) {
	if m == nil || m.Service == nil {
		return nil, fmt.Errorf("skill service未配置")
	}
	pkg := meta.PackageID
	if pkg == "" {
		return nil, fmt.Errorf("package_id为空")
	}
	listed, err := m.Service.ListResources(ctx, skillcontract.ListResourcesRequest{
		ResolveRequest: skillcontract.ResolveRequest{CatalogRequest: catalog, Name: meta.Name, Resource: string(meta.MainResource)},
		PackageID:      pkg,
	})
	if err != nil {
		return nil, fmt.Errorf("列出skill资源失败: %w", err)
	}
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		return nil, err
	}
	out := &Result{SkillDir: skillDir, ScriptsDir: scriptsDir, Files: make([]string, 0), PackageFiles: make([]string, 0)}

	if base := strings.TrimSpace(meta.SourceRef["base_directory"]); base != "" {
		if info, err := os.Stat(base); err == nil && info.IsDir() {
			if err := copyDir(base, skillDir); err != nil {
				return nil, err
			}
			_ = filepath.Walk(skillDir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info == nil || info.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(skillDir, path)
				rel = filepath.ToSlash(rel)
				out.PackageFiles = append(out.PackageFiles, rel)
				if strings.HasPrefix(rel, "scripts/") {
					out.Files = append(out.Files, strings.TrimPrefix(rel, "scripts/"))
				}
				return nil
			})
			if len(out.Files) == 0 {
				return nil, fmt.Errorf("skill %s 没有可 materialize 的 scripts", meta.Name)
			}
			return out, nil
		}
	}

	for _, info := range listed.Resources {
		pkgPrefix := string(pkg) + "/"
		pkgRel := strings.TrimPrefix(string(info.Resource), pkgPrefix)
		if pkgRel == "" || pkgRel == string(info.Resource) || unsafeMaterializedPath(pkgRel) {
			continue
		}
		content, err := m.Service.ReadResource(ctx, skillcontract.ResourceRequest{
			ResolveRequest: skillcontract.ResolveRequest{CatalogRequest: catalog, Name: meta.Name, Resource: string(info.Resource)},
			PackageID:      pkg,
			Resource:       info.Resource,
			MaxBytes:       2 * 1024 * 1024,
		})
		if err != nil {
			return nil, fmt.Errorf("读取脚本资源失败 %s: %w", info.Resource, err)
		}
		dest := filepath.Join(skillDir, filepath.FromSlash(pkgRel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, []byte(content.Content), 0o644); err != nil {
			return nil, err
		}
		out.PackageFiles = append(out.PackageFiles, pkgRel)
		if info.Kind == model.ResourceKindScript && strings.HasPrefix(pkgRel, "scripts/") {
			out.Files = append(out.Files, strings.TrimPrefix(pkgRel, "scripts/"))
		}
	}
	if len(out.Files) == 0 {
		return nil, fmt.Errorf("skill %s 没有可 materialize 的 scripts", meta.Name)
	}
	return out, nil
}

func unsafeMaterializedPath(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return true
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return true
		}
	}
	return false
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
