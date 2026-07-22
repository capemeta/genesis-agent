package materialize

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
func (m *Materializer) MaterializePackageScripts(ctx context.Context, catalog skillcontract.CatalogRequest, meta model.Metadata, expected model.SkillPackageSnapshot, skillDir string) (*Result, error) {
	_ = catalog
	if m == nil || m.Service == nil {
		return nil, fmt.Errorf("skill service未配置")
	}
	pkg := meta.PackageID
	if pkg == "" {
		return nil, fmt.Errorf("package_id为空")
	}
	reader, ok := m.Service.(skillcontract.PackageSnapshotReader)
	if !ok {
		return nil, fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: skill service不支持包快照读取")
	}
	stored, files, err := reader.GetPackageSnapshot(ctx, expected.Digest)
	if err != nil {
		return nil, fmt.Errorf("读取固定 skill package snapshot失败: %w", err)
	}
	if stored.Digest != expected.Digest || stored.PackageID != expected.PackageID || stored.Authority != expected.Authority {
		return nil, fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: package snapshot identity不一致")
	}
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		return nil, err
	}
	out := &Result{SkillDir: skillDir, ScriptsDir: scriptsDir, Files: make([]string, 0), PackageFiles: make([]string, 0)}

	for _, file := range files {
		pkgPrefix := string(pkg) + "/"
		pkgRel := strings.TrimPrefix(string(file.Resource), pkgPrefix)
		if pkgRel == "" || pkgRel == string(file.Resource) || unsafeMaterializedPath(pkgRel) {
			return nil, fmt.Errorf("skill package resource非法: %s", file.Resource)
		}
		dest := filepath.Join(skillDir, filepath.FromSlash(pkgRel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, file.Content, 0o600); err != nil {
			return nil, err
		}
		out.PackageFiles = append(out.PackageFiles, pkgRel)
		if strings.HasPrefix(pkgRel, "scripts/") {
			out.Files = append(out.Files, strings.TrimPrefix(pkgRel, "scripts/"))
		}
	}
	if err := verifyMaterializedSnapshot(skillDir, expected); err != nil {
		return nil, err
	}
	if len(out.Files) == 0 {
		return nil, fmt.Errorf("skill %s 没有可 materialize 的 scripts", meta.Name)
	}
	return out, nil
}

func verifyMaterializedSnapshot(root string, expected model.SkillPackageSnapshot) error {
	if expected.Digest == "" {
		return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: 缺少expected package digest")
	}
	for _, file := range expected.Files {
		prefix := string(expected.PackageID) + "/"
		rel := strings.TrimPrefix(string(file.Resource), prefix)
		if rel == "" || rel == string(file.Resource) || unsafeMaterializedPath(rel) {
			return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: package resource非法: %s", file.Resource)
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: 读取materialized resource %s: %w", file.Resource, err)
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != file.SHA256 || int64(len(content)) != file.Size {
			return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: materialized resource摘要不一致: %s", file.Resource)
		}
	}
	return nil
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
