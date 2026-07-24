package service

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

// ReadBoundResource 从 Binding 固定的内容寻址快照读取文本资源。
func (s *Service) ReadBoundResource(ctx context.Context, req contract.BoundResourceRequest) (model.ResourceContent, error) {
	snapshot, files, err := s.boundPackage(ctx, req.Binding)
	if err != nil {
		return model.ResourceContent{}, err
	}
	resource := model.QualifySkillResource(string(snapshot.PackageID), req.Binding.PhysicalSkill, string(req.Resource))
	if !resourceInPackage(snapshot.PackageID, resource) {
		return model.ResourceContent{}, fmt.Errorf("RESOURCE_NOT_IN_BOUND_PACKAGE: %s不属于binding package %s", resource, snapshot.PackageID)
	}
	for _, file := range files {
		if file.Resource == resource {
			return readBoundFileContent(s.opts.MaxPromptBytes, req, file, resource, snapshot)
		}
	}
	// 容错匹配：当精确匹配未命中时，支持短文件名（如 read.md）或相对后缀唯一匹配
	reqRes := strings.TrimLeft(string(req.Resource), "/")
	var matchFile *model.SkillPackageFile
	matchCount := 0
	for i := range files {
		fRes := string(files[i].Resource)
		if fRes == reqRes || strings.HasSuffix(fRes, "/"+reqRes) || path.Base(fRes) == reqRes {
			matchFile = &files[i]
			matchCount++
		}
	}
	if matchCount == 1 && matchFile != nil {
		return readBoundFileContent(s.opts.MaxPromptBytes, req, *matchFile, matchFile.Resource, snapshot)
	}
	return model.ResourceContent{}, fmt.Errorf("skill resource不存在于binding快照: %s", resource)
}

func readBoundFileContent(maxBytes int, req contract.BoundResourceRequest, file model.SkillPackageFile, resource model.ResourceID, snapshot model.SkillPackageSnapshot) (model.ResourceContent, error) {
	if !utf8.Valid(file.Content) {
		return model.ResourceContent{}, fmt.Errorf("skill resource不是UTF-8文本: %s", resource)
	}
	limit := req.MaxBytes
	if limit <= 0 || limit > maxBytes {
		limit = maxBytes
	}
	content := file.Content
	truncated := len(content) > limit
	if truncated {
		content = content[:limit]
	}
	return model.ResourceContent{Skill: boundMetadata(req.Binding), Resource: resource, Content: string(content), Version: snapshot.Digest, Truncated: truncated}, nil
}

// ListBoundResources 只枚举 Binding 固定快照中的资源。
func (s *Service) ListBoundResources(ctx context.Context, binding model.InvocationBinding) (model.ResourceList, error) {
	_, files, err := s.boundPackage(ctx, binding)
	if err != nil {
		return model.ResourceList{}, err
	}
	resources := make([]model.ResourceInfo, 0, len(files))
	for _, file := range files {
		resources = append(resources, model.ResourceInfo{
			Resource: file.Resource,
			Kind:     boundResourceKind(file.Resource),
			Name:     path.Base(string(file.Resource)),
			Size:     int64(len(file.Content)),
			Text:     utf8.Valid(file.Content),
		})
	}
	sort.SliceStable(resources, func(i, j int) bool { return resources[i].Resource < resources[j].Resource })
	return model.ResourceList{Skill: boundMetadata(binding), Resources: resources}, nil
}

// SearchBoundResources 在 Binding 固定快照中搜索 UTF-8 文本。
func (s *Service) SearchBoundResources(ctx context.Context, req contract.BoundResourceSearchRequest) (model.SearchResult, error) {
	_, files, err := s.boundPackage(ctx, req.Binding)
	if err != nil {
		return model.SearchResult{}, err
	}
	query := strings.ToLower(strings.TrimSpace(req.Query))
	if query == "" {
		return model.SearchResult{}, fmt.Errorf("query不能为空")
	}
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	matches := make([]model.SearchMatch, 0)
	for _, file := range files {
		if !utf8.Valid(file.Content) {
			continue
		}
		text := string(file.Content)
		lower := strings.ToLower(text)
		idx := strings.Index(lower, query)
		if idx < 0 && !strings.Contains(strings.ToLower(string(file.Resource)), query) {
			continue
		}
		if idx < 0 {
			idx = 0
		}
		matches = append(matches, model.SearchMatch{Resource: file.Resource, Title: path.Base(string(file.Resource)), Snippet: boundSnippet(text, idx, len(query))})
		if len(matches) >= limit {
			break
		}
	}
	return model.SearchResult{Skill: boundMetadata(req.Binding), Matches: matches}, nil
}

func (s *Service) boundPackage(ctx context.Context, binding model.InvocationBinding) (model.SkillPackageSnapshot, []model.SkillPackageFile, error) {
	if err := model.ValidateBindingIdentity(binding); err != nil {
		return model.SkillPackageSnapshot{}, nil, fmt.Errorf("SKILL_INVOCATION_BINDING_INVALID: %w", err)
	}
	snapshot, files, err := s.GetPackageSnapshot(ctx, binding.Package.Digest)
	if err != nil {
		return model.SkillPackageSnapshot{}, nil, err
	}
	if snapshot.Digest != binding.Package.Digest || snapshot.PackageID != binding.Package.PackageID || snapshot.Authority != binding.Package.Authority {
		return model.SkillPackageSnapshot{}, nil, fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: binding与package快照身份不一致")
	}
	return snapshot, files, nil
}

func boundMetadata(binding model.InvocationBinding) model.Metadata {
	return model.Metadata{
		Name:      binding.PhysicalSkill,
		Authority: binding.Package.Authority,
		PackageID: binding.Package.PackageID,
		Version:   binding.Package.Version,
	}.Normalize()
}

func resourceInPackage(packageID model.PackageID, resource model.ResourceID) bool {
	prefix := strings.Trim(string(packageID), "/") + "/"
	return strings.HasPrefix(strings.Trim(string(resource), "/"), prefix)
}

func boundResourceKind(resource model.ResourceID) model.ResourceKind {
	parts := strings.Split(strings.Trim(string(resource), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch strings.ToLower(parts[1]) {
	case "references":
		return model.ResourceKindReference
	case "scripts":
		return model.ResourceKindScript
	default:
		return model.ResourceKindAsset
	}
}

func boundSnippet(text string, at, queryLen int) string {
	start := at - 80
	if start < 0 {
		start = 0
	}
	end := at + queryLen + 160
	if end > len(text) {
		end = len(text)
	}
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	for end > start && end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	return strings.TrimSpace(text[start:end])
}
