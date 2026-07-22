// Package service 聚合多来源 Skill，展开 Invocation Catalog，并持久化不可变调用绑定。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	auditcontract "genesis-agent/internal/capabilities/audit/contract"
	auditmodel "genesis-agent/internal/capabilities/audit/model"
	capcontract "genesis-agent/internal/capabilities/capability/contract"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	skillmemory "genesis-agent/internal/capabilities/skill/adapter/memory"
	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	"genesis-agent/internal/capabilities/skill/parser"
	usagecontract "genesis-agent/internal/capabilities/usage/contract"
	usagemodel "genesis-agent/internal/capabilities/usage/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/platform/logger/correl"
)

type Options struct {
	MaxPromptBytes         int
	MaxListBytes           int
	MaxListTokens          int
	SourceTimeout          time.Duration
	AuditSink              auditcontract.Sink
	UsageSink              usagecontract.Sink
	Visibility             capcontract.Registry
	KnownTools             []string
	KnownCapabilities      []string
	KnownQAPolicies        []string
	DefaultInvocationTools []string
	PolicySnapshotVersion  string
	BindingStore           contract.InvocationBindingStore
	PackageStore           contract.SkillPackageSnapshotStore
}

type packageRecord struct {
	physical model.PhysicalSkillDefinition
	source   contract.Source
}

type catalogSnapshot struct {
	catalog  model.Catalog
	packages map[string]packageRecord
}

type Service struct {
	sources []contract.Source
	opts    Options

	mu           sync.RWMutex
	cache        map[string]catalogSnapshot
	packages     map[string]packageRecord
	bindings     contract.InvocationBindingStore
	packageStore contract.SkillPackageSnapshotStore
}

func New(sources []contract.Source, opts Options) *Service {
	if opts.MaxPromptBytes <= 0 {
		opts.MaxPromptBytes = model.MaxPromptBytes
	}
	if opts.MaxListBytes <= 0 {
		opts.MaxListBytes = model.MaxAvailableSkillsSize
	}
	if opts.MaxListTokens <= 0 {
		opts.MaxListTokens = model.MaxAvailableSkillsTokens
	}
	if opts.SourceTimeout <= 0 {
		opts.SourceTimeout = 5 * time.Second
	}
	if strings.TrimSpace(opts.PolicySnapshotVersion) == "" {
		opts.PolicySnapshotVersion = "skill-policy/v1"
	}
	if len(opts.KnownCapabilities) == 0 {
		opts.KnownCapabilities = []string{"vision"}
	}
	if len(opts.KnownQAPolicies) == 0 {
		opts.KnownQAPolicies = []string{"visual-qa/v1"}
	}
	if len(opts.DefaultInvocationTools) == 0 {
		opts.DefaultInvocationTools = []string{
			"list_skill_resources", "read_skill_resource", "search_skill_resources",
			"run_skill_command", "install_skill_dependencies", "read_file", "write_file", "edit_file",
			"view_image", "select_deliverable_candidate",
		}
	}
	if opts.BindingStore == nil {
		opts.BindingStore = skillmemory.NewBindingStore()
	}
	if opts.PackageStore == nil {
		opts.PackageStore = skillmemory.NewPackageStore()
	}
	clean := make([]contract.Source, 0, len(sources))
	for _, source := range sources {
		if source != nil {
			clean = append(clean, source)
		}
	}
	return &Service{
		sources: clean, opts: opts, cache: make(map[string]catalogSnapshot), packages: make(map[string]packageRecord),
		bindings: opts.BindingStore, packageStore: opts.PackageStore,
	}
}

func (s *Service) Catalog(ctx context.Context, req contract.CatalogRequest) (model.Catalog, error) {
	started := time.Now()
	key := cacheKey(req)
	if !req.ForceReload && s.opts.Visibility == nil {
		s.mu.RLock()
		cached, ok := s.cache[key]
		s.mu.RUnlock()
		if ok {
			s.record(ctx, "catalog.list", true, started, catalogMetadata(req, true, len(cached.catalog.Entries)))
			return cloneCatalog(cached.catalog), nil
		}
	}

	catalog := model.Catalog{Entries: make([]model.InvocationMetadata, 0)}
	packages := make(map[string]packageRecord)
	handleOwners := make(map[string][]model.InvocationMetadata)
	for _, snapshot := range s.listSources(ctx, req) {
		if snapshot.err != nil {
			catalog.Errors = append(catalog.Errors, model.Error{Source: snapshot.authority, Message: snapshot.err.Error()})
			continue
		}
		catalog.Errors = append(catalog.Errors, snapshot.result.Errors...)
		catalog.Warnings = append(catalog.Warnings, snapshot.result.Warnings...)
		for _, physical := range snapshot.result.Packages {
			physical.Metadata = physical.Metadata.Normalize()
			if !matchesSkillSet(physical.Metadata, req.EnabledSkills, req.DisabledSkills) || !s.visible(ctx, physical.Metadata) {
				continue
			}
			if err := s.validatePhysical(physical); err != nil {
				catalog.Errors = append(catalog.Errors, model.Error{Source: physical.Metadata.Authority, Path: physical.Metadata.DisplayPath, Message: err.Error()})
				continue
			}
			packageKey := packageIdentity(physical.Metadata.Authority, physical.Metadata.PackageID)
			if _, exists := packages[packageKey]; exists {
				catalog.Errors = append(catalog.Errors, model.Error{Source: physical.Metadata.Authority, Path: physical.Metadata.DisplayPath, Message: "SKILL_INVOCATION_CONFLICT: package identity重复"})
				continue
			}
			packages[packageKey] = packageRecord{physical: physical, source: snapshot.source}
			for _, entry := range expandInvocations(physical, s.opts.DefaultInvocationTools) {
				if !matchesInvocationSet(entry, req.EnabledSkills, req.DisabledSkills) {
					continue
				}
				handleOwners[entry.Name] = append(handleOwners[entry.Name], entry)
			}
		}
	}
	for handle, owners := range handleOwners {
		if len(owners) != 1 {
			for _, owner := range owners {
				catalog.Errors = append(catalog.Errors, model.Error{Source: owner.Authority, Path: string(owner.MainResource), Message: fmt.Sprintf("SKILL_INVOCATION_CONFLICT: handle %q在当前作用域不唯一", handle)})
			}
			continue
		}
		catalog.Entries = append(catalog.Entries, owners[0])
	}
	sort.Slice(catalog.Entries, func(i, j int) bool {
		if scopeRank(catalog.Entries[i].Scope) != scopeRank(catalog.Entries[j].Scope) {
			return scopeRank(catalog.Entries[i].Scope) < scopeRank(catalog.Entries[j].Scope)
		}
		return catalog.Entries[i].Name < catalog.Entries[j].Name
	})

	s.mu.Lock()
	for id, record := range packages {
		s.packages[id] = record
	}
	if len(catalog.Errors) == 0 && s.opts.Visibility == nil {
		s.cache[key] = catalogSnapshot{catalog: cloneCatalog(catalog), packages: clonePackageIndex(packages)}
	}
	s.mu.Unlock()
	s.record(ctx, "catalog.list", true, started, catalogMetadata(req, false, len(catalog.Entries)))
	return catalog, nil
}

func (s *Service) Resolve(ctx context.Context, req contract.ResolveRequest) (model.ResolvedInvocation, error) {
	started := time.Now()
	catalog, err := s.Catalog(ctx, req.CatalogRequest)
	if err != nil {
		return model.ResolvedInvocation{}, err
	}
	query := strings.TrimSpace(req.Name)
	resource := model.NormalizeResourceLocator(req.Resource)
	matches := make([]model.InvocationMetadata, 0, 1)
	for _, entry := range catalog.Entries {
		matched := false
		if query != "" {
			matched = entry.Name == query || entry.QualifiedName == query
			if matched && resource != "" {
				matched = resourceBelongsToEntry(entry, resource)
			}
		} else if resource != "" {
			matched = entryMatchesResource(entry, resource)
		}
		if matched {
			matches = append(matches, entry)
		}
	}
	if len(matches) == 0 {
		return model.ResolvedInvocation{}, fmt.Errorf("SKILL_INVOCATION_NOT_FOUND: %s", firstNonEmpty(query, resource))
	}
	if len(matches) > 1 {
		return model.ResolvedInvocation{}, fmt.Errorf("SKILL_INVOCATION_CONFLICT: %q", firstNonEmpty(query, resource))
	}
	entry := matches[0]
	s.mu.RLock()
	record, ok := s.packages[packageIdentity(entry.Authority, entry.PackageID)]
	s.mu.RUnlock()
	if !ok || record.physical.Snapshot.Digest != entry.PackageDigest {
		return model.ResolvedInvocation{}, fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: package snapshot不存在或已变化")
	}
	definition, profile, err := invocationFromPhysical(record.physical, entry.InvocationID, s.opts.DefaultInvocationTools)
	if err != nil {
		return model.ResolvedInvocation{}, err
	}
	resolved := model.ResolvedInvocation{CatalogItem: entry, Physical: record.physical, Definition: definition, Profile: profile}
	if definition.Prompt.SkillBody == model.SkillBodyInclude {
		read, readErr := record.source.Read(ctx, contract.ReadRequest{Authority: entry.Authority, PackageID: entry.PackageID, Resource: entry.MainResource, Version: entry.Version, MaxBytes: s.opts.MaxPromptBytes})
		if readErr != nil {
			return model.ResolvedInvocation{}, readErr
		}
		if read.Truncated {
			return model.ResolvedInvocation{}, fmt.Errorf("SKILL_MANIFEST_INVALID: SKILL.md超过prompt安全上限，禁止随机截断")
		}
		resolved.SkillBody = read.Content
	}
	if definition.Prompt.Instructions != "" {
		resourceID := model.ResourceID(string(entry.PackageID) + "/" + definition.Prompt.Instructions)
		if !snapshotHasResource(record.physical.Snapshot, resourceID) {
			return model.ResolvedInvocation{}, fmt.Errorf("SKILL_MANIFEST_INVALID: invocation instructions不存在: %s", definition.Prompt.Instructions)
		}
		read, readErr := record.source.Read(ctx, contract.ReadRequest{Authority: entry.Authority, PackageID: entry.PackageID, Resource: resourceID, Version: entry.Version, MaxBytes: model.MaxInvocationInstructions})
		if readErr != nil {
			return model.ResolvedInvocation{}, readErr
		}
		if read.Truncated {
			return model.ResolvedInvocation{}, fmt.Errorf("SKILL_MANIFEST_INVALID: invocation instructions超过%d字节", model.MaxInvocationInstructions)
		}
		resolved.Instructions = read.Content
	}
	s.record(ctx, "invocation.resolve", true, started, invocationMetadata(entry))
	return resolved, nil
}

func (s *Service) CreateBinding(ctx context.Context, req contract.BindingRequest) (model.InvocationBinding, error) {
	if err := validateRequest(req.Resolved.Definition.Request, req.Task, req.Inputs); err != nil {
		return model.InvocationBinding{}, fmt.Errorf("SKILL_REQUEST_INVALID: %w", err)
	}
	if req.Resolved.Physical.Snapshot.Digest == "" || req.Resolved.CatalogItem.PackageDigest != req.Resolved.Physical.Snapshot.Digest {
		return model.InvocationBinding{}, fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: resolved package digest不一致")
	}
	inputs := make([]model.BoundInput, 0, len(req.Inputs))
	for _, ref := range req.Inputs {
		inputs = append(inputs, model.BoundInput{Ref: ref, Alias: ref.Path, SHA256: resourceSHA256(ref.Version)})
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		return model.InvocationBinding{}, fmt.Errorf("SKILL_REQUEST_INVALID: run_id不能为空")
	}
	idempotency := model.BuildIdempotencyKey(req.Resolved.Physical.Snapshot.Digest, req.Resolved.Definition.ID, req.Task, runID, inputs)
	now := time.Now().UTC()
	binding := model.InvocationBinding{
		ID: "skill-binding-" + shortHash(idempotency), TenantID: strings.TrimSpace(req.TenantID), RunID: runID,
		ParentRunID: strings.TrimSpace(req.ParentRunID), Package: req.Resolved.Physical.Snapshot,
		InvocationID: req.Resolved.Definition.ID, Handle: req.Resolved.Definition.Handle, PhysicalSkill: req.Resolved.Physical.Metadata.Name,
		AgentMode: req.Resolved.Definition.AgentMode, RuntimeProfileID: req.Resolved.Definition.RuntimeProfile,
		RuntimeProfile: req.Resolved.Profile, ToolPolicy: req.ToolPolicy, ExecutionPolicy: req.ExecutionPolicy,
		Capabilities: req.Capabilities, Requires: append([]model.CapabilityRequirement(nil), req.Resolved.Definition.Requires...),
		Task: normalizeTask(req.Task), Inputs: inputs, Result: req.Resolved.Definition.Result,
		InstructionDigest: instructionDigest(req.Resolved), PolicySnapshotVersion: firstNonEmpty(req.PolicySnapshotVersion, s.opts.PolicySnapshotVersion),
		IdempotencyKey: idempotency, CreatedAt: now,
	}
	if req.Resolved.Physical.Manifest != nil {
		binding.ManifestSchema = req.Resolved.Physical.Manifest.Schema
	}
	if err := model.ValidateBindingIdentity(binding); err != nil {
		return model.InvocationBinding{}, err
	}
	if err := s.persistResolvedPackage(ctx, req.Resolved); err != nil {
		return model.InvocationBinding{}, err
	}
	if existing, err := s.bindings.GetBindingByIdempotencyKey(ctx, idempotency); err == nil {
		return existing.Clone(), nil
	} else if !errors.Is(err, contract.ErrInvocationBindingNotFound) {
		return model.InvocationBinding{}, err
	}
	return s.bindings.SaveBinding(ctx, binding)
}

// GetPackageSnapshot 只从内容寻址快照存储读取已绑定包，执行阶段不得回读 Source。
func (s *Service) GetPackageSnapshot(ctx context.Context, digest string) (model.SkillPackageSnapshot, []model.SkillPackageFile, error) {
	return s.packageStore.GetPackageSnapshot(ctx, strings.TrimSpace(digest))
}

func (s *Service) persistResolvedPackage(ctx context.Context, resolved model.ResolvedInvocation) error {
	record, err := s.packageRecord(resolved.Physical.Metadata.Authority, resolved.Physical.Metadata.PackageID)
	if err != nil {
		return err
	}
	if record.physical.Snapshot.Digest != resolved.Physical.Snapshot.Digest {
		return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: resolved package已变化")
	}
	reader, ok := record.source.(contract.PackageSnapshotSource)
	if !ok {
		return fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: source不支持不可变包快照")
	}
	files, err := reader.ReadPackageSnapshot(ctx, resolved.Physical.Snapshot)
	if err != nil {
		return fmt.Errorf("固化 skill package snapshot失败: %w", err)
	}
	if err := s.packageStore.SavePackageSnapshot(ctx, resolved.Physical.Snapshot, files); err != nil {
		return fmt.Errorf("保存 skill package snapshot失败: %w", err)
	}
	return nil
}

func resourceSHA256(version string) string {
	value := strings.TrimPrefix(strings.TrimSpace(version), "sha256:")
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return ""
	}
	return strings.ToLower(value)
}

func (s *Service) GetBinding(ctx context.Context, req contract.BindingLookup) (model.InvocationBinding, error) {
	if id := strings.TrimSpace(req.ID); id != "" {
		binding, err := s.bindings.GetBinding(ctx, id)
		if err != nil {
			return model.InvocationBinding{}, contract.ErrInvocationBindingNotFound
		}
		return binding.Clone(), nil
	}
	bindings, err := s.bindings.ListBindingsByRun(ctx, req.TenantID, req.RunID)
	if err != nil {
		return model.InvocationBinding{}, err
	}
	var found model.InvocationBinding
	for _, candidate := range bindings {
		if req.Handle != "" && candidate.Handle != req.Handle && candidate.PhysicalSkill != req.Handle {
			continue
		}
		if found.ID != "" && found.ID != candidate.ID {
			return model.InvocationBinding{}, fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: run内存在多个匹配binding，请使用binding_id")
		}
		found = candidate
	}
	if found.ID == "" {
		return model.InvocationBinding{}, contract.ErrInvocationBindingNotFound
	}
	return found.Clone(), nil
}

func (s *Service) Load(_ context.Context, req contract.LoadRequest) (model.Injection, error) {
	if req.Binding.Package.Digest == "" || req.Binding.Package.Digest != req.Resolved.Physical.Snapshot.Digest || req.Binding.InvocationID != req.Resolved.Definition.ID {
		return model.Injection{}, fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: binding与resolved invocation不一致")
	}
	contents := composePrompt(req.Resolved, req.Binding)
	if len(contents) > s.opts.MaxPromptBytes {
		return model.Injection{}, fmt.Errorf("SKILL_MANIFEST_INVALID: invocation prompt超过%d字节", s.opts.MaxPromptBytes)
	}
	return model.Injection{Skill: req.Resolved.CatalogItem, Binding: req.Binding.Clone(), Resource: req.Resolved.CatalogItem.MainResource, Contents: contents}, nil
}

func (s *Service) ReadResource(ctx context.Context, req contract.ResourceRequest) (model.ResourceContent, error) {
	resolved, err := s.Resolve(ctx, req.ResolveRequest)
	if err != nil {
		return model.ResourceContent{}, err
	}
	packageID := req.PackageID
	if packageID == "" {
		packageID = resolved.Physical.Metadata.PackageID
	}
	resource := req.Resource
	if resource == "" {
		return model.ResourceContent{}, fmt.Errorf("resource不能为空")
	}
	record, err := s.packageRecord(resolved.Physical.Metadata.Authority, packageID)
	if err != nil {
		return model.ResourceContent{}, err
	}
	read, err := record.source.Read(ctx, contract.ReadRequest{Authority: record.physical.Metadata.Authority, PackageID: packageID, Resource: resource, Version: record.physical.Metadata.Version, MaxBytes: req.MaxBytes})
	if err != nil {
		return model.ResourceContent{}, err
	}
	return model.ResourceContent{Skill: record.physical.Metadata, Resource: read.Resource, Content: read.Content, Version: read.Version, Truncated: read.Truncated}, nil
}

func (s *Service) ListResources(ctx context.Context, req contract.ListResourcesRequest) (model.ResourceList, error) {
	resolved, err := s.Resolve(ctx, req.ResolveRequest)
	if err != nil {
		return model.ResourceList{}, err
	}
	record, err := s.packageRecord(resolved.Physical.Metadata.Authority, firstPackage(req.PackageID, resolved.Physical.Metadata.PackageID))
	if err != nil {
		return model.ResourceList{}, err
	}
	listed, err := record.source.ListResources(ctx, contract.SourceListResourcesRequest{Authority: record.physical.Metadata.Authority, PackageID: record.physical.Metadata.PackageID, Version: record.physical.Metadata.Version})
	if err != nil {
		return model.ResourceList{}, err
	}
	return model.ResourceList{Skill: record.physical.Metadata, Resources: listed.Resources}, nil
}

func (s *Service) SearchResources(ctx context.Context, req contract.SearchResourcesRequest) (model.SearchResult, error) {
	resolved, err := s.Resolve(ctx, req.ResolveRequest)
	if err != nil {
		return model.SearchResult{}, err
	}
	record, err := s.packageRecord(resolved.Physical.Metadata.Authority, firstPackage(req.PackageID, resolved.Physical.Metadata.PackageID))
	if err != nil {
		return model.SearchResult{}, err
	}
	searched, err := record.source.Search(ctx, contract.SearchRequest{Authority: record.physical.Metadata.Authority, PackageID: record.physical.Metadata.PackageID, Query: req.Query, Limit: req.Limit})
	if err != nil {
		return model.SearchResult{}, err
	}
	return model.SearchResult{Skill: record.physical.Metadata, Matches: searched.Matches}, nil
}

func (s *Service) SelectForTurn(ctx context.Context, req contract.SelectionRequest) ([]model.InvocationMetadata, error) {
	catalog, err := s.Catalog(ctx, req.CatalogRequest)
	if err != nil {
		return nil, err
	}
	mentions := extractMentions(req.Text)
	selected := make([]model.InvocationMetadata, 0)
	seen := make(map[string]struct{})
	for _, entry := range catalog.Entries {
		for _, name := range mentions.names {
			if name == entry.Name || name == entry.QualifiedName {
				appendSelected(&selected, seen, entry)
			}
		}
		for _, resource := range mentions.resources {
			if entryMatchesResource(entry, resource) {
				appendSelected(&selected, seen, entry)
			}
		}
	}
	return selected, nil
}

func (s *Service) RenderAvailableSkills(ctx context.Context, req contract.CatalogRequest) (string, error) {
	catalog, err := s.Catalog(ctx, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("<available_skills>\n")
	shown := 0
	for _, entry := range catalog.Entries {
		item := fmt.Sprintf("  <skill>\n    <name>%s</name>\n    <description>%s</description>\n    <location>skill://%s</location>\n  </skill>\n", xmlEscape(entry.Name), xmlEscape(oneLine(entry.Description)), xmlEscape(entry.ID))
		if b.Len()+len(item)+len("</available_skills>") > s.opts.MaxListBytes || estimateTokens(b.String()+item) > s.opts.MaxListTokens {
			break
		}
		b.WriteString(item)
		shown++
	}
	if shown < len(catalog.Entries) {
		fmt.Fprintf(&b, "  <truncated>Showing %d of %d</truncated>\n", shown, len(catalog.Entries))
	}
	b.WriteString("</available_skills>")
	return b.String(), nil
}

func (s *Service) ClearCache() {
	s.mu.Lock()
	s.cache = make(map[string]catalogSnapshot)
	s.packages = make(map[string]packageRecord)
	s.mu.Unlock()
}

func (s *Service) validatePhysical(physical model.PhysicalSkillDefinition) error {
	meta := physical.Metadata
	if meta.Name == "" || physical.Snapshot.Digest == "" || physical.Snapshot.Authority != meta.Authority || physical.Snapshot.PackageID != meta.PackageID {
		return fmt.Errorf("SKILL_PACKAGE_UNTRUSTED: package snapshot identity无效")
	}
	if !snapshotHasResource(physical.Snapshot, meta.MainResource) {
		return fmt.Errorf("SKILL_PACKAGE_UNTRUSTED: package digest未覆盖SKILL.md")
	}
	if physical.Manifest != nil {
		opts := parser.ManifestValidationOptions{KnownTools: toSet(s.opts.KnownTools), KnownCapabilities: toSet(s.opts.KnownCapabilities), KnownQAPolicies: toSet(s.opts.KnownQAPolicies)}
		if err := parser.ValidateRuntimeManifest(*physical.Manifest, meta.Name, opts); err != nil {
			return err
		}
		manifestResource := model.ResourceID(string(meta.PackageID) + "/" + model.RuntimeManifestFileName)
		if !snapshotHasResource(physical.Snapshot, manifestResource) || physical.Snapshot.ManifestDigest == "" {
			return fmt.Errorf("SKILL_PACKAGE_UNTRUSTED: package digest未覆盖runtime manifest")
		}
		for _, invocation := range physical.Manifest.Invocations {
			if invocation.Prompt.Instructions == "" {
				continue
			}
			resource := model.ResourceID(string(meta.PackageID) + "/" + invocation.Prompt.Instructions)
			if !snapshotHasResource(physical.Snapshot, resource) {
				return fmt.Errorf("SKILL_MANIFEST_INVALID: invocation instructions不存在: %s", invocation.Prompt.Instructions)
			}
		}
	}
	return nil
}

type sourceSnapshot struct {
	authority model.Authority
	source    contract.Source
	result    contract.ListResult
	err       error
}

func (s *Service) listSources(ctx context.Context, req contract.CatalogRequest) []sourceSnapshot {
	results := make([]sourceSnapshot, len(s.sources))
	var wg sync.WaitGroup
	for i, source := range s.sources {
		wg.Add(1)
		go func(index int, candidate contract.Source) {
			defer wg.Done()
			callCtx, cancel := context.WithTimeout(ctx, s.opts.SourceTimeout)
			defer cancel()
			result, err := candidate.List(callCtx, contract.ListQuery{Product: req.Product, TenantID: req.TenantID, ProjectID: req.ProjectID, AgentID: req.AgentID, UserID: req.UserID, RoleIDs: req.RoleIDs, Environment: req.Environment})
			results[index] = sourceSnapshot{authority: candidate.Authority(), source: candidate, result: result, err: err}
		}(i, source)
	}
	wg.Wait()
	return results
}

func expandInvocations(physical model.PhysicalSkillDefinition, defaultTools []string) []model.InvocationMetadata {
	definitions := defaultDefinitions(physical, defaultTools)
	entries := make([]model.InvocationMetadata, 0, len(definitions))
	for _, definition := range definitions {
		entries = append(entries, model.InvocationMetadata{
			ID:   model.StableInvocationID(physical.Metadata.Authority, physical.Metadata.PackageID, definition.ID),
			Name: definition.Handle, QualifiedName: definition.Handle, Description: definition.Description,
			PhysicalSkill: physical.Metadata.Name, InvocationID: definition.ID, Scope: physical.Metadata.Scope,
			Authority: physical.Metadata.Authority, PackageID: physical.Metadata.PackageID, MainResource: physical.Metadata.MainResource,
			Version: physical.Metadata.Version, PackageDigest: physical.Snapshot.Digest, Enabled: true, PromptVisible: true,
		})
	}
	return entries
}

func defaultDefinitions(physical model.PhysicalSkillDefinition, defaultTools []string) []model.InvocationDefinition {
	if physical.Manifest != nil {
		return append([]model.InvocationDefinition(nil), physical.Manifest.Invocations...)
	}
	return []model.InvocationDefinition{{
		ID: "default", Handle: physical.Metadata.Name, Description: physical.Metadata.Description,
		AgentMode: model.AgentModeSpec{Mode: model.AgentModeMain}, RuntimeProfile: "default",
		Request:    model.RequestContract{Inputs: model.InputContract{Access: model.InputAccessReadOnly, MaxItems: model.MaxInputs}},
		Prompt:     model.InvocationPrompt{SkillBody: model.SkillBodyInclude},
		ToolPolicy: model.ToolPolicy{Allow: append([]string(nil), defaultTools...)}, Result: model.ResultContract{Kind: model.ResultKindMessage},
	}}
}

func invocationFromPhysical(physical model.PhysicalSkillDefinition, id string, defaultTools []string) (model.InvocationDefinition, model.RuntimeProfile, error) {
	for _, definition := range defaultDefinitions(physical, defaultTools) {
		if definition.ID != id {
			continue
		}
		if physical.Manifest == nil {
			return definition, model.RuntimeProfile{
				Sandbox: model.SandboxRequirement{
					Required:      false,
					ExecutionMode: model.ExecutionModePerCall,
					Backends:      []string{"remote_sandbox", "local_platform_sandbox", "local_host"},
				},
			}, nil
		}
		profile := physical.Manifest.RuntimeProfiles[definition.RuntimeProfile]
		profile.Sandbox = parser.NormalizeSandboxRequirement(profile.Sandbox)
		return definition, profile, nil
	}
	return model.InvocationDefinition{}, model.RuntimeProfile{}, fmt.Errorf("SKILL_INVOCATION_NOT_FOUND: %s", id)
}

func validateRequest(contract model.RequestContract, task string, inputs []workmodel.ResourceRef) error {
	if contract.Task.Required && strings.TrimSpace(task) == "" {
		return fmt.Errorf("task不能为空")
	}
	count := len(inputs)
	if count < contract.Inputs.MinItems || (contract.Inputs.MaxItems >= 0 && count > contract.Inputs.MaxItems) {
		return fmt.Errorf("inputs数量%d不在[%d,%d]范围", count, contract.Inputs.MinItems, contract.Inputs.MaxItems)
	}
	for i, ref := range inputs {
		if strings.TrimSpace(ref.Authority) == "" || strings.TrimSpace(ref.Scheme) == "" || strings.TrimSpace(ref.ID) == "" || strings.TrimSpace(ref.Version) == "" {
			return fmt.Errorf("inputs[%d]不是带版本的ResourceRef", i)
		}
		if ref.Path != "" {
			if err := workmodel.WorkspacePath(ref.Path).Validate(); err != nil {
				return fmt.Errorf("inputs[%d] alias非法: %w", i, err)
			}
		}
		if !acceptedInputType(ref, contract.Inputs) {
			return fmt.Errorf("inputs[%d]类型不被接受", i)
		}
	}
	return nil
}

func acceptedInputType(ref workmodel.ResourceRef, input model.InputContract) bool {
	if len(input.AcceptedMIMEs) == 0 && len(input.AcceptedSuffixes) == 0 {
		return true
	}
	for _, mediaType := range input.AcceptedMIMEs {
		if strings.EqualFold(strings.TrimSpace(ref.MediaType), mediaType) {
			return true
		}
	}
	name := strings.ToLower(strings.TrimSpace(ref.Path))
	for _, suffix := range input.AcceptedSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func composePrompt(resolved model.ResolvedInvocation, binding model.InvocationBinding) string {
	var parts []string
	if body := strings.TrimSpace(resolved.SkillBody); body != "" {
		parts = append(parts, "<skill_instruction source=\"SKILL.md\" priority=\"skill\">\n"+body+"\n</skill_instruction>")
	}
	if instructions := strings.TrimSpace(resolved.Instructions); instructions != "" {
		parts = append(parts, "<skill_instruction source=\"invocation\" priority=\"skill\">\n"+instructions+"\n</skill_instruction>")
	}
	var request strings.Builder
	request.WriteString("<invocation_request>\n")
	request.WriteString("handle: " + binding.Handle + "\n")
	if binding.Task != "" {
		request.WriteString("task: " + binding.Task + "\n")
	}
	if len(binding.Inputs) > 0 {
		request.WriteString("inputs:\n")
		for _, input := range binding.Inputs {
			fmt.Fprintf(&request, "- alias=%s resource=%s:%s version=%s\n", input.Alias, input.Ref.Authority, input.Ref.ID, input.Ref.Version)
		}
	}
	request.WriteString("</invocation_request>")
	parts = append(parts, request.String())
	return strings.Join(parts, "\n\n")
}

func (s *Service) packageRecord(authority model.Authority, packageID model.PackageID) (packageRecord, error) {
	s.mu.RLock()
	record, ok := s.packages[packageIdentity(authority, packageID)]
	s.mu.RUnlock()
	if !ok {
		return packageRecord{}, fmt.Errorf("SKILL_BINDING_VERSION_CONFLICT: package snapshot未缓存")
	}
	return record, nil
}

func (s *Service) visible(ctx context.Context, meta model.Metadata) bool {
	if s.opts.Visibility == nil {
		return true
	}
	records, err := s.opts.Visibility.ListCapabilities(ctx, capmodel.CapabilityQuery{Types: []capmodel.CapabilityType{capmodel.CapabilityTypeSkill}, IncludeDisabled: true})
	if err != nil || len(records) == 0 {
		return true
	}
	matched := false
	for _, record := range records {
		if !matchesSkillCapability(meta, record) {
			continue
		}
		matched = true
		if record.Enabled {
			return true
		}
	}
	return !matched
}

func matchesSkillCapability(entry model.Metadata, record capmodel.CapabilityIndexRecord) bool {
	if record.Type != capmodel.CapabilityTypeSkill {
		return false
	}
	if record.Package != "" && (record.Package == string(entry.PackageID) || record.Spec == string(entry.PackageID)) {
		return true
	}
	if record.Name != "" && (record.Name == entry.Name || record.Name == entry.ID) {
		return true
	}
	resource := normalizeVisibilityPath(record.ResourcePath)
	main := normalizeVisibilityPath(string(entry.MainResource))
	return resource != "" && (resource == main || strings.HasSuffix(resource, "/"+main) || strings.HasSuffix(main, "/"+resource))
}

func normalizeVisibilityPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	if idx := strings.Index(value, "skills/"); idx >= 0 {
		value = value[idx+len("skills/"):]
	}
	return strings.Trim(value, "/")
}

func matchesSkillSet(entry model.Metadata, enabled, disabled []string) bool {
	return matchesSet([]string{entry.Name, entry.ID, string(entry.PackageID)}, enabled, disabled)
}

func matchesInvocationSet(entry model.InvocationMetadata, enabled, disabled []string) bool {
	return matchesSet([]string{entry.Name, entry.QualifiedName, entry.ID, entry.PhysicalSkill, string(entry.PackageID)}, enabled, disabled)
}

func matchesSet(keys, enabled, disabled []string) bool {
	matches := func(values []string) bool {
		for _, value := range values {
			value = strings.TrimSpace(value)
			for _, key := range keys {
				if value == "*" || value == key {
					return true
				}
			}
		}
		return false
	}
	if matches(disabled) {
		return false
	}
	return len(enabled) == 0 || matches(enabled)
}

func snapshotHasResource(snapshot model.SkillPackageSnapshot, resource model.ResourceID) bool {
	for _, file := range snapshot.Files {
		if file.Resource == resource {
			return true
		}
	}
	return false
}

func packageIdentity(authority model.Authority, packageID model.PackageID) string {
	return authority.String() + "\x00" + string(packageID)
}

func bindingRunKey(tenantID, runID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(runID)
}

func instructionDigest(resolved model.ResolvedInvocation) string {
	sum := sha256.Sum256([]byte(resolved.SkillBody + "\x00" + resolved.Instructions))
	return hex.EncodeToString(sum[:])
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func cloneCatalog(in model.Catalog) model.Catalog {
	return model.Catalog{Entries: append([]model.InvocationMetadata(nil), in.Entries...), Errors: append([]model.Error(nil), in.Errors...), Warnings: append([]string(nil), in.Warnings...)}
}

func clonePackageIndex(in map[string]packageRecord) map[string]packageRecord {
	out := make(map[string]packageRecord, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func toSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func firstPackage(value, fallback model.PackageID) model.PackageID {
	if value != "" {
		return value
	}
	return fallback
}

func entryMatchesResource(entry model.InvocationMetadata, resource string) bool {
	resource = model.NormalizeResourceLocator(resource)
	return resource != "" && (entry.ID == resource || string(entry.PackageID) == resource || string(entry.MainResource) == resource)
}

func resourceBelongsToEntry(entry model.InvocationMetadata, resource string) bool {
	resource = strings.TrimPrefix(model.NormalizeResourceLocator(resource), "/")
	if entryMatchesResource(entry, resource) {
		return true
	}
	prefix := strings.Trim(string(entry.PackageID), "/") + "/"
	return prefix != "/" && strings.HasPrefix(resource, prefix)
}

func appendSelected(out *[]model.InvocationMetadata, seen map[string]struct{}, entry model.InvocationMetadata) {
	if _, ok := seen[entry.ID]; ok {
		return
	}
	seen[entry.ID] = struct{}{}
	*out = append(*out, entry)
}

type mentions struct{ names, resources []string }

func extractMentions(text string) mentions {
	out := mentions{}
	seenNames, seenResources := map[string]struct{}{}, map[string]struct{}{}
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
		if !isCommonEnvVar(name) {
			if _, exists := seenNames[name]; !exists {
				seenNames[name] = struct{}{}
				out.names = append(out.names, name)
			}
		}
		i = end - 1
	}
	return out
}

func parseLinkedMention(text string, bytes []byte, start int) (string, string, int, bool) {
	if start+2 >= len(bytes) || bytes[start+1] != '$' || !isMentionChar(bytes[start+2]) {
		return "", "", 0, false
	}
	nameStart, nameEnd := start+2, start+3
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
	}
	return false
}

func scopeRank(scope model.Scope) int {
	switch scope {
	case model.ScopeProject:
		return 0
	case model.ScopeAgent:
		return 1
	case model.ScopeUser:
		return 2
	case model.ScopeTenant, model.ScopeOrg:
		return 3
	case model.ScopeAdmin:
		return 4
	case model.ScopeSystem:
		return 5
	default:
		return 9
	}
}

func cacheKey(req contract.CatalogRequest) string {
	parts := []string{string(req.Product), req.TenantID, req.ProjectID, req.AgentID, req.UserID, string(req.Environment), stableList(req.RoleIDs), stableList(req.EnabledSkills), stableList(req.DisabledSkills)}
	return strings.Join(parts, "|")
}

func stableList(values []string) string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return strings.Join(out, ",")
}
func estimateTokens(text string) int { return (utf8.RuneCountInString(text) + 3) / 4 }
func oneLine(value string) string    { return strings.Join(strings.Fields(value), " ") }
func normalizeTask(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}

func catalogMetadata(req contract.CatalogRequest, cacheHit bool, count int) map[string]string {
	return map[string]string{"cache_hit": fmt.Sprintf("%t", cacheHit), "entry_count": fmt.Sprintf("%d", count), "product": string(req.Product), "tenant_id": req.TenantID, "project_id": req.ProjectID, "user_id": req.UserID, "agent_id": req.AgentID, "environment": string(req.Environment)}
}

func invocationMetadata(entry model.InvocationMetadata) map[string]string {
	return map[string]string{"skill.handle": entry.Name, "skill.physical": entry.PhysicalSkill, "skill.invocation_id": entry.InvocationID, "skill.authority": entry.Authority.String(), "skill.package_id": string(entry.PackageID), "skill.package_digest": entry.PackageDigest}
}

func (s *Service) record(ctx context.Context, action string, success bool, started time.Time, metadata map[string]string) {
	completed := time.Now()
	runID, sessionID, metadata := correl.Enrich(ctx, "", "", metadata)
	if s.opts.AuditSink != nil {
		_ = s.opts.AuditSink.Record(ctx, auditmodel.Event{Category: "skill", Action: "skill." + action, RunID: runID, SessionID: sessionID, Severity: severity(success), Allowed: success, StartedAt: started, CompletedAt: completed, DurationMS: completed.Sub(started).Milliseconds(), Metadata: cloneMap(metadata)})
	}
	if s.opts.UsageSink != nil {
		_ = s.opts.UsageSink.RecordToolUsage(ctx, usagemodel.ToolUsage{ToolName: "skill." + action, Success: success, ReadOnly: true, StartedAt: started, CompletedAt: completed, DurationMS: completed.Sub(started).Milliseconds(), RunID: runID, SessionID: sessionID, Metadata: cloneMap(metadata)})
	}
}

func severity(success bool) auditmodel.Severity {
	if success {
		return auditmodel.SeverityInfo
	}
	return auditmodel.SeverityWarn
}
func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

var _ contract.Service = (*Service)(nil)
