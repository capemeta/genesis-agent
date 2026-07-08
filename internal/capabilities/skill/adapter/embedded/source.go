// Package embedded 提供基于 fs.FS 的内置 Skill Source。
package embedded

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

const skillFileName = "SKILL.md"

type Source struct {
	authority model.Authority
	scope     model.Scope
	root      fs.FS
	parser    contract.Parser
}

func NewSource(authority model.Authority, scope model.Scope, root fs.FS, parser contract.Parser) (*Source, error) {
	if root == nil {
		return nil, fmt.Errorf("embedded skill fs不能为空")
	}
	if parser == nil {
		return nil, fmt.Errorf("skill parser不能为空")
	}
	if authority.Kind == "" {
		authority.Kind = model.SourceKindEmbedded
	}
	if authority.ID == "" {
		authority.ID = "system"
	}
	if scope == "" {
		scope = model.ScopeSystem
	}
	return &Source{authority: authority, scope: scope, root: root, parser: parser}, nil
}

func (s *Source) Authority() model.Authority { return s.authority }

func (s *Source) List(ctx context.Context, query contract.ListQuery) (contract.ListResult, error) {
	select {
	case <-ctx.Done():
		return contract.ListResult{}, ctx.Err()
	default:
	}
	entries, err := fs.ReadDir(s.root, ".")
	if err != nil {
		if errorsIsNotExist(err) {
			return contract.ListResult{}, nil
		}
		return contract.ListResult{}, err
	}
	result := contract.ListResult{Entries: make([]model.Metadata, 0)}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := s.readMetadata(entry.Name())
		if err != nil {
			result.Errors = append(result.Errors, model.Error{Source: s.authority, Path: entry.Name(), Message: err.Error()})
			continue
		}
		result.Entries = append(result.Entries, meta)
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
	meta, body, err := s.readFull(pkg)
	if err != nil {
		return contract.ReadResult{}, err
	}
	if resource == string(meta.MainResource) {
		content, truncated := truncateUTF8(body, req.MaxBytes)
		return contract.ReadResult{Metadata: meta, Resource: meta.MainResource, Content: content, Truncated: truncated, Version: meta.Version}, nil
	}
	content, truncated, err := s.readResource(pkg, model.ResourceID(resource), req.MaxBytes)
	if err != nil {
		return contract.ReadResult{}, err
	}
	return contract.ReadResult{Metadata: meta, Resource: model.ResourceID(resource), Content: content, Truncated: truncated, Version: meta.Version}, nil
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
	meta, _, err := s.readFull(pkg)
	if err != nil {
		return contract.ListResourcesResult{}, err
	}
	resources := make([]model.ResourceInfo, 0)
	err = fs.WalkDir(s.root, pkg, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		resource := model.ResourceID(p)
		if !isAllowedResource(resource) {
			return nil
		}
		info := model.ResourceInfo{Resource: resource, Kind: resourceKind(resource), Name: path.Base(p)}
		if stat, err := entry.Info(); err == nil {
			info.Size = stat.Size()
		}
		if data, err := fs.ReadFile(s.root, p); err == nil && utf8.Valid(data) {
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
func (s *Source) Search(ctx context.Context, req contract.SearchRequest) (contract.SearchResult, error) {
	select {
	case <-ctx.Done():
		return contract.SearchResult{}, ctx.Err()
	default:
	}
	pkg := strings.TrimSpace(string(req.PackageID))
	query := strings.TrimSpace(strings.ToLower(req.Query))
	if pkg == "" || query == "" {
		return contract.SearchResult{}, nil
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	matches := make([]model.SearchMatch, 0)
	err := fs.WalkDir(s.root, pkg, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || len(matches) >= limit {
			return walkErr
		}
		resource := model.ResourceID(p)
		if !isAllowedResource(resource) {
			return nil
		}
		data, err := fs.ReadFile(s.root, p)
		if err != nil || !utf8.Valid(data) {
			return nil
		}
		text := string(data)
		idx := strings.Index(strings.ToLower(text), query)
		if idx < 0 {
			return nil
		}
		matches = append(matches, model.SearchMatch{Resource: resource, Title: path.Base(p), Snippet: snippet(text, idx, len(query))})
		return nil
	})
	if err != nil {
		return contract.SearchResult{}, err
	}
	return contract.SearchResult{Matches: matches}, nil
}

func (s *Source) readMetadata(pkg string) (model.Metadata, error) {
	if err := model.ValidateName(pkg); err != nil {
		return model.Metadata{}, err
	}
	data, err := readSkillFile(s.root, pkg, 64*1024)
	if err != nil {
		return model.Metadata{}, err
	}
	return s.parser.ParseFrontmatter(data, contract.ParseSource{Authority: s.authority, Scope: s.scope, PackageID: model.PackageID(pkg), MainResource: model.ResourceID(pkg + "/SKILL.md"), DisplayPath: pkg + "/SKILL.md", DirectoryName: pkg})
}

func (s *Source) readFull(pkg string) (model.Metadata, string, error) {
	data, err := fs.ReadFile(s.root, pkg+"/"+skillFileName)
	if err != nil {
		data, err = fs.ReadFile(s.root, pkg+"/skill.md")
	}
	if err != nil {
		return model.Metadata{}, "", err
	}
	return s.parser.ParseFull(data, contract.ParseSource{Authority: s.authority, Scope: s.scope, PackageID: model.PackageID(pkg), MainResource: model.ResourceID(pkg + "/SKILL.md"), DisplayPath: pkg + "/SKILL.md", DirectoryName: pkg})
}

func (s *Source) readResource(pkg string, resource model.ResourceID, maxBytes int) (string, bool, error) {
	if !isAllowedResource(resource) {
		return "", false, fmt.Errorf("skill resource不允许读取: %s", resource)
	}
	value := path.Clean(strings.TrimSpace(string(resource)))
	if !strings.HasPrefix(value, pkg+"/") {
		return "", false, fmt.Errorf("resource不属于package: %s", resource)
	}
	data, err := fs.ReadFile(s.root, value)
	if err != nil {
		return "", false, err
	}
	if !utf8.Valid(data) {
		return "", false, fmt.Errorf("skill resource不是UTF-8文本: %s", resource)
	}
	content, truncated := truncateUTF8(string(data), maxBytes)
	return content, truncated, nil
}

func readSkillFile(root fs.FS, pkg string, maxBytes int) ([]byte, error) {
	for _, name := range []string{skillFileName, "skill.md"} {
		data, err := fs.ReadFile(root, pkg+"/"+name)
		if err == nil {
			if maxBytes > 0 && len(data) > maxBytes {
				return data[:maxBytes], nil
			}
			return data, nil
		}
	}
	return nil, fs.ErrNotExist
}

func isAllowedResource(resource model.ResourceID) bool {
	value := path.Clean(strings.TrimSpace(string(resource)))
	parts := strings.Split(value, "/")
	if len(parts) < 3 || parts[0] == "" || strings.Contains(parts[0], "..") {
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

func (s *Source) isTextResource(p string) (bool, error) {
	file, err := s.root.Open(p)
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
	parts := strings.Split(path.Clean(strings.TrimSpace(string(resource))), "/")
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
func truncateUTF8(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value, false
	}
	cut := maxBytes
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut], true
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

func errorsIsNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
