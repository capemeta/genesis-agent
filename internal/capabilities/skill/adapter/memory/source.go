// Package memory 提供进程内 Skill Source，适合测试、开发态和产品动态注入。
package memory

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

// Skill 描述一个内存 Skill 包。
type Skill struct {
	Metadata  model.Metadata
	Body      string
	Resources map[model.ResourceID]string
}

// Source 是线程安全的内存 Skill 来源。
type Source struct {
	authority model.Authority

	mu     sync.RWMutex
	skills map[model.PackageID]Skill
}

func NewSource(authority model.Authority, skills []Skill) *Source {
	if authority.Kind == "" {
		authority.Kind = model.SourceKindEmbedded
	}
	if authority.ID == "" {
		authority.ID = "memory"
	}
	s := &Source{authority: authority, skills: make(map[model.PackageID]Skill, len(skills))}
	for _, skill := range skills {
		s.Put(skill)
	}
	return s
}

func (s *Source) Authority() model.Authority { return s.authority }

func (s *Source) Put(skill Skill) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta := skill.Metadata
	if meta.Authority.Kind == "" {
		meta.Authority = s.authority
	}
	if meta.PackageID == "" {
		meta.PackageID = model.PackageID(meta.Name)
	}
	if meta.MainResource == "" {
		meta.MainResource = model.ResourceID(string(meta.PackageID) + "/SKILL.md")
	}
	skill.Metadata = meta.Normalize()
	if skill.Resources == nil {
		skill.Resources = map[model.ResourceID]string{}
	}
	s.skills[skill.Metadata.PackageID] = skill
}
func (s *Source) List(ctx context.Context, query contract.ListQuery) (contract.ListResult, error) {
	select {
	case <-ctx.Done():
		return contract.ListResult{}, ctx.Err()
	default:
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]model.Metadata, 0, len(s.skills))
	for _, skill := range s.skills {
		entries = append(entries, skill.Metadata.Normalize())
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].QualifiedName < entries[j].QualifiedName })
	return contract.ListResult{Entries: entries}, nil
}

func (s *Source) Read(ctx context.Context, req contract.ReadRequest) (contract.ReadResult, error) {
	select {
	case <-ctx.Done():
		return contract.ReadResult{}, ctx.Err()
	default:
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	skill, ok := s.skills[req.PackageID]
	if !ok {
		return contract.ReadResult{}, fmt.Errorf("未找到skill package: %s", req.PackageID)
	}
	resource := req.Resource
	if resource == "" || resource == skill.Metadata.MainResource {
		content, truncated := truncateUTF8(skill.Body, req.MaxBytes)
		return contract.ReadResult{Metadata: skill.Metadata, Resource: skill.Metadata.MainResource, Content: content, Truncated: truncated, Version: skill.Metadata.Version}, nil
	}
	content, ok := skill.Resources[resource]
	if !ok {
		return contract.ReadResult{}, fmt.Errorf("未找到skill resource: %s", resource)
	}
	content, truncated := truncateUTF8(content, req.MaxBytes)
	return contract.ReadResult{Metadata: skill.Metadata, Resource: resource, Content: content, Truncated: truncated, Version: skill.Metadata.Version}, nil
}

func (s *Source) ListResources(ctx context.Context, req contract.SourceListResourcesRequest) (contract.ListResourcesResult, error) {
	select {
	case <-ctx.Done():
		return contract.ListResourcesResult{}, ctx.Err()
	default:
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	skill, ok := s.skills[req.PackageID]
	if !ok {
		return contract.ListResourcesResult{}, fmt.Errorf("未找到skill package: %s", req.PackageID)
	}
	resources := make([]model.ResourceInfo, 0, len(skill.Resources))
	for resource, content := range skill.Resources {
		resources = append(resources, model.ResourceInfo{Resource: resource, Kind: resourceKind(resource), Name: resourceName(resource), Size: int64(len(content)), Text: utf8.ValidString(content)})
	}
	sort.SliceStable(resources, func(i, j int) bool { return resources[i].Resource < resources[j].Resource })
	return contract.ListResourcesResult{Metadata: skill.Metadata, Resources: resources, Version: skill.Metadata.Version}, nil
}
func (s *Source) Search(ctx context.Context, req contract.SearchRequest) (contract.SearchResult, error) {
	select {
	case <-ctx.Done():
		return contract.SearchResult{}, ctx.Err()
	default:
	}
	query := strings.TrimSpace(strings.ToLower(req.Query))
	if query == "" {
		return contract.SearchResult{}, nil
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	skill, ok := s.skills[req.PackageID]
	if !ok {
		return contract.SearchResult{}, fmt.Errorf("未找到skill package: %s", req.PackageID)
	}
	matches := make([]model.SearchMatch, 0)
	for resource, content := range skill.Resources {
		pathLower := strings.ToLower(string(resource))
		baseLower := strings.ToLower(path.Base(string(resource)))
		idx := strings.Index(strings.ToLower(content), query)
		pathHit := strings.Contains(pathLower, query) || strings.Contains(baseLower, query)
		if idx < 0 && !pathHit {
			continue
		}
		snippetAt := idx
		if snippetAt < 0 {
			snippetAt = 0
		}
		matches = append(matches, model.SearchMatch{Resource: resource, Title: string(resource), Snippet: snippet(content, snippetAt, len(query))})
		if len(matches) >= limit {
			break
		}
	}
	return contract.SearchResult{Matches: matches}, nil
}

func resourceKind(resource model.ResourceID) model.ResourceKind {
	parts := strings.Split(strings.TrimSpace(string(resource)), "/")
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

func resourceName(resource model.ResourceID) string {
	parts := strings.Split(strings.TrimSpace(string(resource)), "/")
	if len(parts) == 0 {
		return string(resource)
	}
	return parts[len(parts)-1]
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
