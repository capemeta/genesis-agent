// Package skill 提供本地主机 Skill 来源实现。
package skill

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

const skillFileName = "SKILL.md"

// Root 描述一个本地 Skill 根目录。
type Root struct {
	Path  string
	Scope model.Scope
}

// Source 从本机目录读取 Skill。它只出现在 shared/local，不进入 internal 能力域。
type Source struct {
	authority model.Authority
	roots     []Root
	parser    contract.Parser
}

// NewSource 创建本地 Skill 来源。
func NewSource(authority model.Authority, roots []Root, parser contract.Parser) (*Source, error) {
	if authority.Kind == "" {
		authority.Kind = model.SourceKindHost
	}
	if authority.ID == "" {
		authority.ID = "local"
	}
	if parser == nil {
		return nil, fmt.Errorf("skill parser不能为空")
	}
	clean := make([]Root, 0, len(roots))
	for _, root := range roots {
		path := strings.TrimSpace(root.Path)
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("解析skill root失败: %w", err)
		}
		abs = filepath.Clean(abs)
		if root.Scope == "" {
			root.Scope = model.ScopeProject
		}
		clean = append(clean, Root{Path: abs, Scope: root.Scope})
	}
	return &Source{authority: authority, roots: clean, parser: parser}, nil
}

func (s *Source) Authority() model.Authority { return s.authority }

func (s *Source) List(ctx context.Context, query contract.ListQuery) (contract.ListResult, error) {
	result := contract.ListResult{Entries: make([]model.Metadata, 0)}
	for _, root := range s.roots {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		entries, err := os.ReadDir(root.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			result.Errors = append(result.Errors, model.Error{Source: s.authority, Path: root.Path, Message: err.Error()})
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			meta, err := s.readMetadata(root, entry.Name(), true)
			if err != nil {
				result.Errors = append(result.Errors, model.Error{Source: s.authority, Path: filepath.Join(root.Path, entry.Name()), Message: err.Error()})
				continue
			}
			result.Entries = append(result.Entries, meta)
		}
	}
	sort.SliceStable(result.Entries, func(i, j int) bool { return result.Entries[i].QualifiedName < result.Entries[j].QualifiedName })
	return result, nil
}

func (s *Source) Read(ctx context.Context, req contract.ReadRequest) (contract.ReadResult, error) {
	select {
	case <-ctx.Done():
		return contract.ReadResult{}, ctx.Err()
	default:
	}
	pkg := strings.TrimSpace(string(req.PackageID))
	if pkg == "" {
		return contract.ReadResult{}, fmt.Errorf("package_id不能为空")
	}
	resource := strings.TrimSpace(string(req.Resource))
	if resource == "" {
		resource = pkg + "/SKILL.md"
	}
	for _, root := range s.roots {
		meta, body, baseDir, err := s.readFull(root, pkg)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return contract.ReadResult{}, err
		}
		if resource == string(meta.MainResource) {
			return contract.ReadResult{Metadata: meta, Resource: meta.MainResource, Content: body}, nil
		}
		content, truncated, err := s.readPackageResource(baseDir, pkg, model.ResourceID(resource), req.MaxBytes)
		if err != nil {
			return contract.ReadResult{}, err
		}
		return contract.ReadResult{Metadata: meta, Resource: model.ResourceID(resource), Content: content, Truncated: truncated}, nil
	}
	return contract.ReadResult{}, fmt.Errorf("未找到skill package: %s", pkg)
}

func (s *Source) ListResources(ctx context.Context, req contract.SourceListResourcesRequest) (contract.ListResourcesResult, error) {
	select {
	case <-ctx.Done():
		return contract.ListResourcesResult{}, ctx.Err()
	default:
	}
	pkg := strings.TrimSpace(string(req.PackageID))
	if pkg == "" {
		return contract.ListResourcesResult{}, fmt.Errorf("package_id不能为空")
	}
	for _, root := range s.roots {
		meta, _, baseDir, err := s.readFull(root, pkg)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return contract.ListResourcesResult{}, err
		}
		resources := make([]model.ResourceInfo, 0)
		err = filepath.WalkDir(baseDir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			resource, ok := resourceIDForPath(baseDir, pkg, path)
			if !ok || !isAllowedResource(resource) {
				return nil
			}
			info := model.ResourceInfo{Resource: resource, Kind: resourceKind(resource), Name: filepath.Base(path)}
			if stat, err := entry.Info(); err == nil {
				info.Size = stat.Size()
			}
			if data, err := os.ReadFile(path); err == nil && utf8.Valid(data) {
				info.Text = true
			}
			resources = append(resources, info)
			return nil
		})
		if err != nil {
			return contract.ListResourcesResult{}, err
		}
		sort.SliceStable(resources, func(i, j int) bool { return resources[i].Resource < resources[j].Resource })
		return contract.ListResourcesResult{Metadata: meta, Resources: resources, Version: meta.Version}, nil
	}
	return contract.ListResourcesResult{}, fmt.Errorf("未找到skill package: %s", pkg)
}
func (s *Source) Search(ctx context.Context, req contract.SearchRequest) (contract.SearchResult, error) {
	pkg := strings.TrimSpace(string(req.PackageID))
	query := strings.TrimSpace(req.Query)
	if pkg == "" || query == "" {
		return contract.SearchResult{}, nil
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	matches := make([]model.SearchMatch, 0)
	for _, root := range s.roots {
		_, _, baseDir, err := s.readFull(root, pkg)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return contract.SearchResult{}, err
		}
		err = filepath.WalkDir(baseDir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() || len(matches) >= limit {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			resource, ok := resourceIDForPath(baseDir, pkg, path)
			if !ok || !isAllowedResource(resource) {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil || !utf8.Valid(data) {
				return nil
			}
			text := string(data)
			idx := strings.Index(strings.ToLower(text), strings.ToLower(query))
			if idx < 0 {
				return nil
			}
			matches = append(matches, model.SearchMatch{Resource: resource, Title: filepath.Base(path), Snippet: snippet(text, idx, len(query))})
			return nil
		})
		if err != nil {
			return contract.SearchResult{}, err
		}
		if len(matches) >= limit {
			break
		}
	}
	return contract.SearchResult{Matches: matches}, nil
}

func (s *Source) readMetadata(root Root, dirName string, allowSymlink bool) (model.Metadata, error) {
	skillFile, baseDir, err := s.resolveSkillFile(root.Path, dirName, allowSymlink)
	if err != nil {
		return model.Metadata{}, err
	}
	file, err := os.Open(skillFile)
	if err != nil {
		return model.Metadata{}, err
	}
	defer file.Close()
	buf := make([]byte, 64*1024)
	n, err := file.Read(buf)
	if err != nil && n == 0 {
		return model.Metadata{}, err
	}
	return s.parser.ParseFrontmatter(buf[:n], contract.ParseSource{
		Authority:     s.authority,
		Scope:         root.Scope,
		PackageID:     model.PackageID(dirName),
		MainResource:  model.ResourceID(dirName + "/SKILL.md"),
		DisplayPath:   skillFile,
		BaseDirectory: baseDir,
		DirectoryName: dirName,
	})
}

func (s *Source) readFull(root Root, dirName string) (model.Metadata, string, string, error) {
	skillFile, baseDir, err := s.resolveSkillFile(root.Path, dirName, true)
	if err != nil {
		return model.Metadata{}, "", "", err
	}
	data, err := os.ReadFile(skillFile)
	if err != nil {
		return model.Metadata{}, "", "", err
	}
	meta, body, err := s.parser.ParseFull(data, contract.ParseSource{
		Authority:     s.authority,
		Scope:         root.Scope,
		PackageID:     model.PackageID(dirName),
		MainResource:  model.ResourceID(dirName + "/SKILL.md"),
		DisplayPath:   skillFile,
		BaseDirectory: baseDir,
		DirectoryName: dirName,
	})
	return meta, body, baseDir, err
}

func (s *Source) readPackageResource(baseDir, pkg string, resource model.ResourceID, maxBytes int) (string, bool, error) {
	if !isAllowedResource(resource) {
		return "", false, fmt.Errorf("skill resource不允许读取: %s", resource)
	}
	path, err := pathForResource(baseDir, pkg, resource)
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	truncated := false
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[:maxBytes]
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
		truncated = true
	}
	if !utf8.Valid(data) {
		return "", false, fmt.Errorf("skill resource不是UTF-8文本: %s", resource)
	}
	return string(data), truncated, nil
}

func (s *Source) resolveSkillFile(rootPath, dirName string, allowSymlink bool) (string, string, error) {
	if err := model.ValidateName(dirName); err != nil {
		return "", "", err
	}
	candidate := filepath.Join(rootPath, dirName)
	info, err := os.Lstat(candidate)
	if err != nil {
		return "", "", err
	}
	baseDir := candidate
	if info.Mode()&os.ModeSymlink != 0 {
		if !allowSymlink {
			return "", "", fmt.Errorf("不允许skill目录符号链接: %s", candidate)
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return "", "", err
		}
		if !isWithin(rootPath, resolved) {
			return "", "", fmt.Errorf("skill符号链接不能指向root外部: %s", candidate)
		}
		baseDir = resolved
	} else if !info.IsDir() {
		return "", "", fmt.Errorf("skill package不是目录: %s", candidate)
	}
	for _, name := range []string{skillFileName, "skill.md"} {
		path := filepath.Join(baseDir, name)
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return path, baseDir, nil
		}
	}
	return "", "", os.ErrNotExist
}

func isAllowedResource(resource model.ResourceID) bool {
	value := filepath.ToSlash(strings.TrimSpace(string(resource)))
	parts := strings.Split(value, "/")
	if len(parts) < 3 {
		return false
	}
	if parts[0] == "" || strings.Contains(parts[0], "..") {
		return false
	}
	switch parts[1] {
	case "references", "assets", "scripts":
		return true
	default:
		return false
	}
}

const textProbeBytes = 8 * 1024

func isTextFile(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, textProbeBytes+1))
	if err != nil {
		return false, err
	}
	return utf8.Valid(data), nil
}
func resourceKind(resource model.ResourceID) model.ResourceKind {
	parts := strings.Split(filepath.ToSlash(strings.TrimSpace(string(resource))), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[1] {
	case "references":
		return model.ResourceKindReference
	case "scripts":
		return model.ResourceKindScript
	case "assets":
		return model.ResourceKindAsset
	default:
		return ""
	}
}
func pathForResource(baseDir, pkg string, resource model.ResourceID) (string, error) {
	value := filepath.ToSlash(strings.TrimSpace(string(resource)))
	prefix := pkg + "/"
	if !strings.HasPrefix(value, prefix) {
		return "", fmt.Errorf("resource不属于package: %s", resource)
	}
	rel := strings.TrimPrefix(value, prefix)
	if strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
		return "", fmt.Errorf("非法skill resource: %s", resource)
	}
	path := filepath.Join(baseDir, filepath.FromSlash(rel))
	if !isWithin(baseDir, path) {
		return "", fmt.Errorf("skill resource越界: %s", resource)
	}
	return path, nil
}

func resourceIDForPath(baseDir, pkg, path string) (model.ResourceID, bool) {
	rel, err := filepath.Rel(baseDir, path)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	return model.ResourceID(pkg + "/" + filepath.ToSlash(rel)), true
}

func snippet(text string, index, queryLen int) string {
	start := index - 80
	if start < 0 {
		start = 0
	}
	end := index + queryLen + 80
	if end > len(text) {
		end = len(text)
	}
	return strings.Join(strings.Fields(text[start:end]), " ")
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
	rootAbs = filepath.Clean(rootAbs)
	pathAbs = filepath.Clean(pathAbs)
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}
