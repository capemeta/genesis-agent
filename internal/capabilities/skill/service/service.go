// Package service 聚合多来源 Skill，并提供缓存、解析和正文加载。
package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	auditmodel "genesis-agent/internal/capabilities/audit/model"
	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	usagecontract "genesis-agent/internal/capabilities/usage/contract"
	usagemodel "genesis-agent/internal/capabilities/usage/model"
)

// Options 控制 Skill Service 行为。
type Options struct {
	MaxPromptBytes int
	MaxListBytes   int
	SourceTimeout  time.Duration
	AuditSink      auditcontract.Sink
	UsageSink      usagecontract.Sink
}

// Service 是产品无关的 Skill 编排服务。
type Service struct {
	sources []contract.Source
	opts    Options

	mu    sync.RWMutex
	cache map[string]model.Catalog
}

// New 创建 Skill Service。
func New(sources []contract.Source, opts Options) *Service {
	if opts.MaxPromptBytes <= 0 {
		opts.MaxPromptBytes = model.MaxPromptBytes
	}
	if opts.MaxListBytes <= 0 {
		opts.MaxListBytes = model.MaxAvailableSkillsSize
	}
	if opts.SourceTimeout <= 0 {
		opts.SourceTimeout = 5 * time.Second
	}
	clean := make([]contract.Source, 0, len(sources))
	for _, source := range sources {
		if source != nil {
			clean = append(clean, source)
		}
	}
	return &Service{sources: clean, opts: opts, cache: make(map[string]model.Catalog)}
}

func (s *Service) Catalog(ctx context.Context, req contract.CatalogRequest) (model.Catalog, error) {
	started := time.Now()
	key := cacheKey(req)
	if !req.ForceReload {
		s.mu.RLock()
		if cached, ok := s.cache[key]; ok {
			s.mu.RUnlock()
			s.record(ctx, "catalog.list", true, started, catalogMetadata(req, true, len(cached.Entries)))
			return cloneCatalog(cached), nil
		}
		s.mu.RUnlock()
	}

	catalog := model.Catalog{Entries: make([]model.Metadata, 0)}
	seen := map[string]model.Metadata{}
	for _, snapshot := range s.listSources(ctx, req) {
		if snapshot.err != nil {
			catalog.Errors = append(catalog.Errors, model.Error{Source: snapshot.authority, Message: snapshot.err.Error()})
			s.record(ctx, "source.error", false, started, map[string]string{"skill.authority": snapshot.authority.String(), "error": snapshot.err.Error()})
			continue
		}
		catalog.Errors = append(catalog.Errors, snapshot.result.Errors...)
		for _, sourceErr := range snapshot.result.Errors {
			s.record(ctx, "source.error", false, started, map[string]string{"skill.authority": sourceErr.Source.String(), "error": sourceErr.Message})
		}
		for _, warning := range snapshot.result.Warnings {
			if strings.TrimSpace(warning) != "" {
				catalog.Warnings = append(catalog.Warnings, warning)
			}
		}
		for _, entry := range snapshot.result.Entries {
			entry = entry.Normalize()
			if !entry.Enabled || !entry.Policy.MatchesProduct(req.Product) {
				continue
			}
			if !matchesSkillSet(entry, req.EnabledSkills, req.DisabledSkills) {
				continue
			}
			stableKey := entry.Authority.String() + ":" + string(entry.PackageID)
			if _, ok := seen[stableKey]; ok {
				continue
			}
			seen[stableKey] = entry
			catalog.Entries = append(catalog.Entries, entry)
		}
	}
	sort.SliceStable(catalog.Entries, func(i, j int) bool {
		li, lj := scopeRank(catalog.Entries[i].Scope), scopeRank(catalog.Entries[j].Scope)
		if li != lj {
			return li < lj
		}
		return catalog.Entries[i].QualifiedName < catalog.Entries[j].QualifiedName
	})

	if len(catalog.Errors) == 0 {
		s.mu.Lock()
		s.cache[key] = cloneCatalog(catalog)
		s.mu.Unlock()
	}
	meta := catalogMetadata(req, false, len(catalog.Entries))
	meta["source_count"] = fmt.Sprintf("%d", len(s.sources))
	s.record(ctx, "catalog.list", true, started, meta)
	return catalog, nil
}

type sourceSnapshot struct {
	index     int
	authority model.Authority
	result    contract.ListResult
	err       error
}

func (s *Service) listSources(ctx context.Context, req contract.CatalogRequest) []sourceSnapshot {
	if len(s.sources) == 0 {
		return nil
	}
	out := make([]sourceSnapshot, len(s.sources))
	pending := make(map[int]model.Authority, len(s.sources))
	ch := make(chan sourceSnapshot, len(s.sources))
	query := contract.ListQuery{Product: req.Product, TenantID: req.TenantID, ProjectID: req.ProjectID, AgentID: req.AgentID, UserID: req.UserID, RoleIDs: append([]string(nil), req.RoleIDs...), Environment: req.Environment}
	listCtx, cancel := context.WithTimeout(ctx, s.opts.SourceTimeout)
	defer cancel()
	for index, source := range s.sources {
		index, source := index, source
		pending[index] = source.Authority()
		go func() {
			result, err := source.List(listCtx, query)
			ch <- sourceSnapshot{index: index, authority: source.Authority(), result: result, err: err}
		}()
	}
	for len(pending) > 0 {
		select {
		case snapshot := <-ch:
			if _, ok := pending[snapshot.index]; !ok {
				continue
			}
			delete(pending, snapshot.index)
			out[snapshot.index] = snapshot
		case <-listCtx.Done():
			for index, authority := range pending {
				out[index] = sourceSnapshot{index: index, authority: authority, err: listCtx.Err()}
			}
			return out
		}
	}
	return out
}
func (s *Service) Resolve(ctx context.Context, req contract.ResolveRequest) (model.Metadata, error) {
	started := time.Now()
	selected, err := s.resolve(ctx, req)
	metadata := map[string]string{"skill.query": firstNonEmpty(req.Name, req.Resource)}
	if err == nil {
		metadata = skillMetadata(selected)
	}
	s.record(ctx, "resolve", err == nil, started, metadata)
	return selected, err
}

func (s *Service) Load(ctx context.Context, req contract.LoadRequest) (model.Injection, error) {
	started := time.Now()
	meta, err := s.resolve(ctx, req.ResolveRequest)
	if err != nil {
		s.record(ctx, "load", false, started, map[string]string{"skill.query": firstNonEmpty(req.Name, req.Resource)})
		return model.Injection{}, err
	}
	source := s.sourceFor(meta.Authority)
	if source == nil {
		err := fmt.Errorf("skill source不可用: %s", meta.Authority.String())
		s.record(ctx, "load", false, started, skillMetadata(meta))
		return model.Injection{}, err
	}
	read, err := source.Read(ctx, contract.ReadRequest{PackageID: meta.PackageID, Resource: meta.MainResource, MaxBytes: s.opts.MaxPromptBytes})
	if err != nil {
		s.record(ctx, "load", false, started, skillMetadata(meta))
		return model.Injection{}, err
	}
	contents := formatContents(read.Metadata, read.Content, req.Args)
	contents, truncated := truncateUTF8(contents, s.opts.MaxPromptBytes)
	out := model.Injection{Skill: read.Metadata, Resource: read.Resource, Contents: contents, Args: req.Args, Truncated: truncated || read.Truncated}
	metadata := skillMetadata(out.Skill)
	metadata["truncated"] = fmt.Sprintf("%t", out.Truncated)
	s.record(ctx, "load", true, started, metadata)
	return out, nil
}

func (s *Service) ReadResource(ctx context.Context, req contract.ResourceRequest) (model.ResourceContent, error) {
	started := time.Now()
	meta, err := s.resolveResourceOwner(ctx, req.ResolveRequest, req.PackageID)
	if err != nil {
		s.record(ctx, "resource.read", false, started, nil)
		return model.ResourceContent{}, err
	}
	source := s.sourceFor(meta.Authority)
	if source == nil {
		return model.ResourceContent{}, fmt.Errorf("skill source不可用: %s", meta.Authority.String())
	}
	read, err := source.Read(ctx, contract.ReadRequest{PackageID: meta.PackageID, Resource: req.Resource, MaxBytes: req.MaxBytes})
	if err != nil {
		s.record(ctx, "resource.read", false, started, skillMetadata(meta))
		return model.ResourceContent{}, err
	}
	out := model.ResourceContent{Skill: meta, Resource: read.Resource, Content: read.Content, Version: read.Version, Truncated: read.Truncated}
	metadata := skillMetadata(meta)
	metadata["skill.resource_id"] = string(read.Resource)
	metadata["truncated"] = fmt.Sprintf("%t", read.Truncated)
	s.record(ctx, "resource.read", true, started, metadata)
	return out, nil
}

func (s *Service) SearchResources(ctx context.Context, req contract.SearchResourcesRequest) (model.SearchResult, error) {
	started := time.Now()
	meta, err := s.resolveResourceOwner(ctx, req.ResolveRequest, req.PackageID)
	if err != nil {
		s.record(ctx, "resource.search", false, started, nil)
		return model.SearchResult{}, err
	}
	source := s.sourceFor(meta.Authority)
	if source == nil {
		return model.SearchResult{}, fmt.Errorf("skill source不可用: %s", meta.Authority.String())
	}
	result, err := source.Search(ctx, contract.SearchRequest{PackageID: meta.PackageID, Query: req.Query, Limit: req.Limit})
	if err != nil {
		s.record(ctx, "resource.search", false, started, skillMetadata(meta))
		return model.SearchResult{}, err
	}
	metadata := skillMetadata(meta)
	metadata["match_count"] = fmt.Sprintf("%d", len(result.Matches))
	s.record(ctx, "resource.search", true, started, metadata)
	return model.SearchResult{Skill: meta, Matches: result.Matches}, nil
}

func (s *Service) SelectForTurn(ctx context.Context, req contract.SelectionRequest) ([]model.Metadata, error) {
	catalog, err := s.Catalog(ctx, req.CatalogRequest)
	if err != nil {
		return nil, err
	}
	mentions := extractMentions(req.Text)
	selected := make([]model.Metadata, 0)
	seen := map[string]struct{}{}

	for _, resource := range mentions.resources {
		for _, entry := range catalog.Entries {
			if entryMatchesResource(entry, resource) {
				appendSelected(&selected, seen, entry)
			}
		}
	}
	for _, name := range mentions.names {
		matches := make([]model.Metadata, 0, 1)
		for _, entry := range catalog.Entries {
			if !entry.Policy.AllowsImplicitInvocation() {
				continue
			}
			if entry.Name == name || entry.QualifiedName == name {
				matches = append(matches, entry)
			}
		}
		if len(matches) == 1 {
			appendSelected(&selected, seen, matches[0])
		}
	}
	return selected, nil
}
func (s *Service) RenderAvailableSkills(ctx context.Context, req contract.CatalogRequest) (string, error) {
	catalog, err := s.Catalog(ctx, req)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("## Skills\n")
	sb.WriteString("可按需调用 `load_skill` 加载下列技能的完整说明；不要臆造未列出的技能。\n")
	count := 0
	omitted := 0
	for _, entry := range catalog.Entries {
		if !entry.PromptVisible {
			continue
		}
		line := fmt.Sprintf("- %s: %s (%s: %s)\n", entry.QualifiedName, oneLine(entry.Description), entry.Authority.Kind, entry.MainResource)
		if sb.Len()+len(line) > s.opts.MaxListBytes {
			omitted++
			continue
		}
		sb.WriteString(line)
		count++
	}
	if count == 0 {
		return "", nil
	}
	if omitted > 0 {
		sb.WriteString(fmt.Sprintf("- %d additional skills omitted from this bounded skills list.\n", omitted))
	}
	return sb.String(), nil
}

func (s *Service) ClearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = make(map[string]model.Catalog)
}

func (s *Service) resolve(ctx context.Context, req contract.ResolveRequest) (model.Metadata, error) {
	catalog, err := s.Catalog(ctx, req.CatalogRequest)
	if err != nil {
		return model.Metadata{}, err
	}
	query := strings.TrimSpace(req.Name)
	resource := model.NormalizeResourceLocator(req.Resource)
	if query == "" && resource == "" {
		return model.Metadata{}, fmt.Errorf("skill name或resource不能为空")
	}
	matches := make([]model.Metadata, 0, 1)
	for _, entry := range catalog.Entries {
		if resource != "" && entryMatchesResource(entry, resource) {
			matches = append(matches, entry)
			continue
		}
		if query != "" && (entry.Name == query || entry.QualifiedName == query || entry.ID == query || string(entry.PackageID) == query) {
			matches = append(matches, entry)
		}
	}
	if len(matches) == 0 {
		return model.Metadata{}, fmt.Errorf("未找到skill: %s", firstNonEmpty(query, resource))
	}
	if len(matches) > 1 && !strings.Contains(query, ":") {
		return model.Metadata{}, fmt.Errorf("skill名称 %q 有多个匹配，请使用qualified_name或resource", query)
	}
	selected := matches[0]
	if req.ModelCall && selected.Policy.DisableModelInvocation {
		return model.Metadata{}, fmt.Errorf("skill %q 禁止模型直接调用", selected.QualifiedName)
	}
	return selected, nil
}

func (s *Service) resolveResourceOwner(ctx context.Context, req contract.ResolveRequest, packageID model.PackageID) (model.Metadata, error) {
	if packageID != "" {
		catalog, err := s.Catalog(ctx, req.CatalogRequest)
		if err != nil {
			return model.Metadata{}, err
		}
		for _, entry := range catalog.Entries {
			if entry.PackageID == packageID {
				return entry, nil
			}
		}
		return model.Metadata{}, fmt.Errorf("未找到skill package: %s", packageID)
	}
	return s.resolve(ctx, req)
}

func appendSelected(out *[]model.Metadata, seen map[string]struct{}, entry model.Metadata) {
	key := entry.Authority.String() + ":" + string(entry.PackageID)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*out = append(*out, entry)
}

type mentions struct {
	names     []string
	resources []string
}

func extractMentions(text string) mentions {
	out := mentions{}
	seenNames := map[string]struct{}{}
	seenResources := map[string]struct{}{}
	bytes := []byte(text)
	for i := 0; i < len(bytes); i++ {
		if bytes[i] == '[' {
			name, resource, end, ok := parseLinkedMention(text, bytes, i)
			if ok {
				if _, exists := seenNames[name]; !exists {
					seenNames[name] = struct{}{}
					out.names = append(out.names, name)
				}
				resource = model.NormalizeResourceLocator(resource)
				if _, exists := seenResources[resource]; !exists {
					seenResources[resource] = struct{}{}
					out.resources = append(out.resources, resource)
				}
				i = end - 1
				continue
			}
		}
		if bytes[i] != '$' {
			continue
		}
		start := i + 1
		if start >= len(bytes) || !isMentionChar(bytes[start]) {
			continue
		}
		end := start + 1
		for end < len(bytes) && isMentionChar(bytes[end]) {
			end++
		}
		name := text[start:end]
		if isCommonEnvVar(name) {
			continue
		}
		if _, exists := seenNames[name]; !exists {
			seenNames[name] = struct{}{}
			out.names = append(out.names, name)
		}
		i = end - 1
	}
	return out
}

func parseLinkedMention(text string, bytes []byte, start int) (string, string, int, bool) {
	if start+2 >= len(bytes) || bytes[start+1] != '$' || !isMentionChar(bytes[start+2]) {
		return "", "", 0, false
	}
	nameStart := start + 2
	nameEnd := nameStart + 1
	for nameEnd < len(bytes) && isMentionChar(bytes[nameEnd]) {
		nameEnd++
	}
	if nameEnd >= len(bytes) || bytes[nameEnd] != ']' {
		return "", "", 0, false
	}
	pathStart := nameEnd + 1
	for pathStart < len(bytes) && (bytes[pathStart] == ' ' || bytes[pathStart] == '\t') {
		pathStart++
	}
	if pathStart >= len(bytes) || bytes[pathStart] != '(' {
		return "", "", 0, false
	}
	pathEnd := pathStart + 1
	for pathEnd < len(bytes) && bytes[pathEnd] != ')' {
		pathEnd++
	}
	if pathEnd >= len(bytes) {
		return "", "", 0, false
	}
	resource := strings.TrimSpace(text[pathStart+1 : pathEnd])
	if resource == "" {
		return "", "", 0, false
	}
	return text[nameStart:nameEnd], resource, pathEnd + 1, true
}

func isMentionChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == ':'
}

func isCommonEnvVar(name string) bool {
	switch strings.ToUpper(name) {
	case "PATH", "HOME", "USER", "SHELL", "PWD", "TMPDIR", "TEMP", "TMP", "LANG", "TERM":
		return true
	default:
		return false
	}
}

func entryMatchesResource(entry model.Metadata, resource string) bool {
	resource = model.NormalizeResourceLocator(resource)
	return resource != "" && (string(entry.MainResource) == resource || entry.ID == resource || entry.DisplayPath == resource || string(entry.PackageID) == resource)
}
func (s *Service) sourceFor(authority model.Authority) contract.Source {
	for _, candidate := range s.sources {
		if candidate.Authority() == authority {
			return candidate
		}
	}
	return nil
}

func catalogMetadata(req contract.CatalogRequest, cacheHit bool, entryCount int) map[string]string {
	return map[string]string{
		"cache_hit":   fmt.Sprintf("%t", cacheHit),
		"entry_count": fmt.Sprintf("%d", entryCount),
		"product":     string(req.Product),
		"tenant_id":   req.TenantID,
		"project_id":  req.ProjectID,
		"user_id":     req.UserID,
		"agent_id":    req.AgentID,
		"environment": string(req.Environment),
	}
}
func (s *Service) record(ctx context.Context, action string, success bool, started time.Time, metadata map[string]string) {
	completed := time.Now()
	if metadata == nil {
		metadata = map[string]string{}
	}
	if s.opts.AuditSink != nil {
		_ = s.opts.AuditSink.Record(ctx, auditmodel.Event{Category: "skill", Action: "skill." + action, Severity: severity(success), Allowed: success, StartedAt: started, CompletedAt: completed, DurationMS: completed.Sub(started).Milliseconds(), Metadata: cloneMap(metadata)})
	}
	if s.opts.UsageSink != nil {
		_ = s.opts.UsageSink.RecordToolUsage(ctx, usagemodel.ToolUsage{ToolName: "skill." + action, Success: success, ReadOnly: true, StartedAt: started, CompletedAt: completed, DurationMS: completed.Sub(started).Milliseconds(), Metadata: cloneMap(metadata)})
	}
}

func severity(success bool) auditmodel.Severity {
	if success {
		return auditmodel.SeverityInfo
	}
	return auditmodel.SeverityWarn
}

func skillMetadata(entry model.Metadata) map[string]string {
	return map[string]string{
		"skill.name":           entry.Name,
		"skill.qualified_name": entry.QualifiedName,
		"skill.scope":          string(entry.Scope),
		"skill.authority":      entry.Authority.String(),
		"skill.authority.kind": string(entry.Authority.Kind),
		"skill.authority.id":   entry.Authority.ID,
		"skill.package_id":     string(entry.PackageID),
		"skill.resource_id":    string(entry.MainResource),
		"skill.version":        entry.Version,
	}
}

func cloneMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func matchesSkillSet(entry model.Metadata, enabled, disabled []string) bool {
	nameMatches := func(values []string) bool {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if value == "*" || value == entry.Name || value == entry.QualifiedName || value == entry.ID || value == string(entry.PackageID) {
				return true
			}
		}
		return false
	}
	if nameMatches(disabled) {
		return false
	}
	if len(enabled) == 0 {
		return true
	}
	return nameMatches(enabled)
}

func cacheKey(req contract.CatalogRequest) string {
	parts := []string{string(req.Product), req.TenantID, req.ProjectID, req.AgentID, req.UserID, string(req.Environment)}
	parts = append(parts, stableList(req.RoleIDs), stableList(req.EnabledSkills), stableList(req.DisabledSkills))
	return strings.Join(parts, "|")
}

func stableList(values []string) string {
	if len(values) == 0 {
		return ""
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	return strings.Join(out, ",")
}

func cloneCatalog(in model.Catalog) model.Catalog {
	out := model.Catalog{}
	out.Entries = append(out.Entries, in.Entries...)
	out.Errors = append(out.Errors, in.Errors...)
	out.Warnings = append(out.Warnings, in.Warnings...)
	return out
}

func scopeRank(scope model.Scope) int {
	switch scope {
	case model.ScopeProject:
		return 10
	case model.ScopeUser:
		return 20
	case model.ScopePlugin:
		return 30
	case model.ScopeSystem:
		return 40
	case model.ScopeAdmin:
		return 50
	case model.ScopeTenant:
		return 60
	case model.ScopeOrg:
		return 70
	case model.ScopeAgent:
		return 80
	case model.ScopeSession:
		return 90
	default:
		return 100
	}
}

func formatContents(meta model.Metadata, body, args string) string {
	var sb strings.Builder
	if base := meta.SourceRef["base_directory"]; base != "" {
		sb.WriteString("Base directory for this skill: ")
		sb.WriteString(base)
		sb.WriteString("\n\n")
	}
	if strings.TrimSpace(args) != "" {
		sb.WriteString("Arguments: ")
		sb.WriteString(args)
		sb.WriteString("\n\n")
	}
	sb.WriteString(strings.TrimSpace(body))
	return sb.String()
}

func truncateUTF8(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value, false
	}
	marker := "\n\n[skill内容已按预算截断]"
	cut := maxBytes
	if maxBytes > len(marker) {
		cut = maxBytes - len(marker)
	}
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	if maxBytes <= len(marker) {
		return value[:cut], true
	}
	return value[:cut] + marker, true
}

func oneLine(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len([]rune(value)) <= 240 {
		return value
	}
	runes := []rune(value)
	return string(runes[:240]) + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
