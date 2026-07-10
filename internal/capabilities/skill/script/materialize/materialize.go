package materialize

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

// Result 是脚本树落盘结果。
type Result struct {
	SkillDir   string
	ScriptsDir string
	Files      []string
}

// Materializer 将 Skill scripts/ 落到可执行目录。
type Materializer struct {
	Service skillcontract.Service
	// SharedScriptsFS 可选。根为共享包的 scripts/（含 office/），合并进目标 Skill。
	SharedScriptsFS fs.FS
	// SharedForPrefixes 触发共享合并的 skill name 前缀，默认 office-。
	SharedForPrefixes []string
}

// MaterializePackageScripts 把包内全部 scripts 资源写到 skillDir/scripts。
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
	out := &Result{SkillDir: skillDir, ScriptsDir: scriptsDir, Files: make([]string, 0)}
	seen := map[string]struct{}{}

	// 磁盘 Skill：若 base_directory 存在，优先复制 scripts 目录（更快且保留非 UTF-8）。
	if base := strings.TrimSpace(meta.SourceRef["base_directory"]); base != "" {
		srcScripts := filepath.Join(base, "scripts")
		if info, err := os.Stat(srcScripts); err == nil && info.IsDir() {
			if err := copyDir(srcScripts, scriptsDir); err != nil {
				return nil, err
			}
			_ = filepath.Walk(scriptsDir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info == nil || info.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(scriptsDir, path)
				relSlash := filepath.ToSlash(rel)
				seen[relSlash] = struct{}{}
				out.Files = append(out.Files, relSlash)
				return nil
			})
			if err := m.mergeSharedScripts(meta.Name, scriptsDir, seen, out); err != nil {
				return nil, err
			}
			if len(out.Files) == 0 {
				return nil, fmt.Errorf("skill %s 没有可 materialize 的 scripts", meta.Name)
			}
			return out, nil
		}
	}
	useSharedOffice := m.SharedScriptsFS != nil && needsSharedOffice(meta.Name, m.SharedForPrefixes)
	for _, info := range listed.Resources {
		if info.Kind != model.ResourceKindScript {
			continue
		}
		rel := strings.TrimPrefix(string(info.Resource), string(pkg)+"/scripts/")
		rel = strings.TrimPrefix(rel, "scripts/")
		if rel == "" || strings.Contains(rel, "..") {
			continue
		}
		// 共享 office/ 由 SharedScriptsFS 二进制安全合并，避免经 UTF-8 ReadResource。
		if useSharedOffice && (rel == "office" || strings.HasPrefix(rel, "office/")) {
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
		dest := filepath.Join(scriptsDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, []byte(content.Content), 0o644); err != nil {
			return nil, err
		}
		seen[rel] = struct{}{}
		out.Files = append(out.Files, rel)
	}
	if err := m.mergeSharedScripts(meta.Name, scriptsDir, seen, out); err != nil {
		return nil, err
	}
	if len(out.Files) == 0 {
		return nil, fmt.Errorf("skill %s 没有可 materialize 的 scripts", meta.Name)
	}
	return out, nil
}

func (m *Materializer) mergeSharedScripts(skillName, scriptsDir string, seen map[string]struct{}, out *Result) error {
	if m.SharedScriptsFS == nil || !needsSharedOffice(skillName, m.SharedForPrefixes) {
		return nil
	}
	return fs.WalkDir(m.SharedScriptsFS, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel := path.Clean(p)
		if rel == "." || strings.Contains(rel, "..") {
			return nil
		}
		if strings.Contains(rel, "__pycache__") || strings.HasSuffix(rel, ".pyc") {
			return nil
		}
		if _, ok := seen[rel]; ok {
			// Skill 包内同名文件优先，不覆盖。
			return nil
		}
		data, err := fs.ReadFile(m.SharedScriptsFS, p)
		if err != nil {
			return fmt.Errorf("读取共享脚本失败 %s: %w", p, err)
		}
		dest := filepath.Join(scriptsDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return err
		}
		seen[rel] = struct{}{}
		out.Files = append(out.Files, rel)
		return nil
	})
}

func needsSharedOffice(skillName string, prefixes []string) bool {
	name := strings.TrimSpace(skillName)
	if name == "" {
		return false
	}
	if len(prefixes) == 0 {
		prefixes = []string{"office-"}
	}
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(name, p) {
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
