package service

import (
	"context"
	"strings"
	"testing"

	skillmemory "genesis-agent/internal/capabilities/skill/adapter/memory"
	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestCatalogExpandsPhysicalSkillIntoInvocationHandles(t *testing.T) {
	svc := testService(t)
	catalog, err := svc.Catalog(context.Background(), contract.CatalogRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Errors) != 0 || len(catalog.Entries) != 2 {
		t.Fatalf("catalog=%+v", catalog)
	}
	if catalog.Entries[0].Name != "demo" || catalog.Entries[1].Name != "demo-read" {
		t.Fatalf("entries=%+v", catalog.Entries)
	}
	for _, entry := range catalog.Entries {
		if entry.PhysicalSkill != "demo" || entry.PackageDigest == "" {
			t.Fatalf("entry=%+v", entry)
		}
	}
}

func TestResolveLoadsInvocationSpecificInstructionsAndSkillBody(t *testing.T) {
	svc := testService(t)
	read, err := svc.Resolve(context.Background(), contract.ResolveRequest{Name: "demo-read"})
	if err != nil {
		t.Fatal(err)
	}
	if read.SkillBody != "" || !strings.Contains(read.Instructions, "只读") {
		t.Fatalf("read=%+v", read)
	}
	work, err := svc.Resolve(context.Background(), contract.ResolveRequest{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if work.SkillBody != "portable body" || !strings.Contains(work.Instructions, "制作") {
		t.Fatalf("work=%+v", work)
	}
}

func TestCreateBindingValidatesRequestPersistsAndIsIdempotent(t *testing.T) {
	store := skillmemory.NewBindingStore()
	svc := testServiceWithStore(t, store)
	resolved, err := svc.Resolve(context.Background(), contract.ResolveRequest{Name: "demo-read"})
	if err != nil {
		t.Fatal(err)
	}
	input := workmodel.ResourceRef{Authority: "host", Scheme: "run-input", ID: "requirements", Version: "sha256:abc", Path: "requirements.pptx"}
	req := contract.BindingRequest{
		Resolved: resolved, TenantID: "tenant", RunID: "run", Inputs: []workmodel.ResourceRef{input},
		ToolPolicy:      model.EffectiveToolPolicy{Base: []string{"run_skill_command"}, Allowed: []string{"run_skill_command"}, Required: []string{"run_skill_command"}},
		ExecutionPolicy: model.EffectiveExecutionPolicy{SandboxRequired: true, ExecutionMode: model.ExecutionModePerCall},
		Capabilities:    model.EffectiveCapabilitySnapshot{VisionMode: "degraded_text"},
	}
	first, err := svc.CreateBinding(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateBinding(context.Background(), req)
	if err != nil || first.ID != second.ID || first.IdempotencyKey != second.IdempotencyKey {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
	loaded, err := svc.GetBinding(context.Background(), contract.BindingLookup{TenantID: "tenant", RunID: "run", Handle: "demo-read"})
	if err != nil || loaded.Package.Digest != resolved.Physical.Snapshot.Digest {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	bad := req
	bad.Inputs = nil
	if _, err := svc.CreateBinding(context.Background(), bad); err == nil || !strings.Contains(err.Error(), "SKILL_REQUEST_INVALID") {
		t.Fatalf("expected request validation, got %v", err)
	}
}

func TestLoadRejectsBindingSnapshotMismatch(t *testing.T) {
	svc := testService(t)
	resolved, _ := svc.Resolve(context.Background(), contract.ResolveRequest{Name: "demo"})
	binding, err := svc.CreateBinding(context.Background(), contract.BindingRequest{
		Resolved: resolved, RunID: "run", Task: "制作", ToolPolicy: model.EffectiveToolPolicy{Allowed: []string{"run_skill_command"}},
		ExecutionPolicy: model.EffectiveExecutionPolicy{SandboxRequired: true, ExecutionMode: model.ExecutionModeSandboxedSession},
	})
	if err != nil {
		t.Fatal(err)
	}
	injection, err := svc.Load(context.Background(), contract.LoadRequest{Resolved: resolved, Binding: binding})
	if err != nil || !strings.Contains(injection.Contents, "portable body") || !strings.Contains(injection.Contents, "制作") {
		t.Fatalf("injection=%+v err=%v", injection, err)
	}
	binding.Package.Digest = "sha256:tampered"
	if _, err := svc.Load(context.Background(), contract.LoadRequest{Resolved: resolved, Binding: binding}); err == nil {
		t.Fatal("tampered binding must fail")
	}
}

func TestBindingPackageSnapshotSurvivesSourceUpgrade(t *testing.T) {
	authority := model.Authority{Kind: model.SourceKindEmbedded, ID: "upgrade-test"}
	manifest := testManifest()
	source := skillmemory.NewSource(authority, []skillmemory.Skill{{
		Metadata: model.Metadata{Name: "demo", Description: "Demo documents", Authority: authority, Scope: model.ScopeSystem, PackageID: "demo", MainResource: "demo/SKILL.md"},
		Body:     "old body", Manifest: &manifest,
		Resources: map[model.ResourceID]string{"demo/references/read.md": "只读提取", "demo/references/work.md": "完整制作"},
	}})
	packages := skillmemory.NewPackageStore()
	svc := New([]contract.Source{source}, Options{KnownTools: []string{"run_skill_command", "view_image"}, DefaultInvocationTools: []string{"run_skill_command"}, BindingStore: skillmemory.NewBindingStore(), PackageStore: packages})
	resolved, err := svc.Resolve(context.Background(), contract.ResolveRequest{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := svc.CreateBinding(context.Background(), contract.BindingRequest{
		Resolved: resolved, RunID: "run-old", Task: "制作", ToolPolicy: model.EffectiveToolPolicy{Allowed: []string{"run_skill_command"}},
		ExecutionPolicy: model.EffectiveExecutionPolicy{SandboxRequired: true, ExecutionMode: model.ExecutionModeSandboxedSession},
	})
	if err != nil {
		t.Fatal(err)
	}
	source.Put(skillmemory.Skill{
		Metadata: resolved.Physical.Metadata, Body: "new body", Manifest: &manifest,
		Resources: map[model.ResourceID]string{"demo/references/read.md": "只读提取", "demo/references/work.md": "完整制作"},
	})
	svc.ClearCache()
	newResolved, err := svc.Resolve(context.Background(), contract.ResolveRequest{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if newResolved.Physical.Snapshot.Digest == binding.Package.Digest {
		t.Fatal("source upgrade must create a new package digest")
	}
	stored, files, err := svc.GetPackageSnapshot(context.Background(), binding.Package.Digest)
	if err != nil || stored.Digest != binding.Package.Digest {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	foundOld := false
	for _, file := range files {
		if file.Resource == "demo/SKILL.md" && strings.Contains(string(file.Content), "old body") && !strings.Contains(string(file.Content), "new body") {
			foundOld = true
		}
	}
	if !foundOld {
		t.Fatalf("old immutable package not retained: %+v", files)
	}
	boundRead, err := svc.ReadBoundResource(context.Background(), contract.BoundResourceRequest{Binding: binding, Resource: "SKILL.md"})
	if err != nil || !strings.Contains(boundRead.Content, "old body") || strings.Contains(boundRead.Content, "new body") {
		t.Fatalf("bound read drifted from immutable snapshot: content=%q err=%v", boundRead.Content, err)
	}
	boundList, err := svc.ListBoundResources(context.Background(), binding)
	if err != nil || len(boundList.Resources) == 0 {
		t.Fatalf("bound list=%+v err=%v", boundList, err)
	}
	boundSearch, err := svc.SearchBoundResources(context.Background(), contract.BoundResourceSearchRequest{Binding: binding, Query: "old body"})
	if err != nil || len(boundSearch.Matches) != 1 || boundSearch.Matches[0].Resource != "demo/SKILL.md" {
		t.Fatalf("bound search=%+v err=%v", boundSearch, err)
	}
}

func TestRenderAvailableSkillsFiltersAncestorSkills(t *testing.T) {
	svc := testService(t)
	// Normal render without ancestor context
	renderedAll, err := svc.RenderAvailableSkills(context.Background(), contract.CatalogRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(renderedAll, "demo") || !strings.Contains(renderedAll, "demo-read") {
		t.Fatalf("renderedAll=%s", renderedAll)
	}

	// Render with ancestor context containing 'demo'
	ctx := contract.WithInvocationAncestors(context.Background(), []string{"test:demo:work"})
	renderedFiltered, err := svc.RenderAvailableSkills(ctx, contract.CatalogRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(renderedFiltered, "<name>demo</name>") || strings.Contains(renderedFiltered, "<name>demo-read</name>") {
		t.Fatalf("ancestor skills should be filtered out without comments, got=%s", renderedFiltered)
	}
}

func TestComposePromptCleansAbsoluteHostPaths(t *testing.T) {
	resolved := model.ResolvedInvocation{
		SkillBody:    "Skill instructions",
		Instructions: "Do work",
	}
	binding := model.InvocationBinding{
		Task: `请解析文件 C:\Users\csl2021\Documents\test.pptx 并转换`,
		Inputs: []model.BoundInput{
			{
				Alias: "input_1.pptx",
				Ref: workmodel.ResourceRef{
					Authority: "host",
					Scheme:    "file",
					ID:        `C:\Users\csl2021\Documents\test.pptx`,
					Path:      `C:\Users\csl2021\Documents\test.pptx`,
				},
			},
		},
	}
	got := composePrompt(resolved, binding)
	if strings.Contains(got, `C:\Users\csl2021`) {
		t.Fatalf("composePrompt leaked host absolute path:\n%s", got)
	}
	if !strings.Contains(got, "input_1.pptx") {
		t.Fatalf("composePrompt missing replaced alias:\n%s", got)
	}
}


func testService(t *testing.T) *Service {
	t.Helper()
	return testServiceWithStore(t, skillmemory.NewBindingStore())
}

func testServiceWithStore(t *testing.T, store contract.InvocationBindingStore) *Service {
	t.Helper()
	authority := model.Authority{Kind: model.SourceKindEmbedded, ID: "test"}
	manifest := testManifest()
	source := skillmemory.NewSource(authority, []skillmemory.Skill{{
		Metadata: model.Metadata{Name: "demo", Description: "Demo documents", Authority: authority, Scope: model.ScopeSystem, PackageID: "demo", MainResource: "demo/SKILL.md"},
		Body:     "portable body", Manifest: &manifest,
		Resources: map[model.ResourceID]string{"demo/references/read.md": "只读提取", "demo/references/work.md": "完整制作"},
	}})
	return New([]contract.Source{source}, Options{KnownTools: []string{"run_skill_command", "view_image"}, DefaultInvocationTools: []string{"run_skill_command"}, BindingStore: store})
}

func testManifest() model.RuntimeManifest {
	return model.RuntimeManifest{
		Schema: model.RuntimeManifestSchemaV1, Skill: "demo",
		RuntimeProfiles: map[string]model.RuntimeProfile{
			"read": {Sandbox: model.SandboxRequirement{Required: true, ExecutionMode: model.ExecutionModePerCall}},
			"work": {Sandbox: model.SandboxRequirement{Required: true, ExecutionMode: model.ExecutionModeSandboxedSession}},
		},
		Invocations: []model.InvocationDefinition{
			{ID: "read", Handle: "demo-read", Description: "Read demo documents", AgentMode: model.AgentModeSpec{Mode: model.AgentModeMain}, RuntimeProfile: "read", Request: model.RequestContract{Inputs: model.InputContract{MinItems: 1, MaxItems: 1, Access: model.InputAccessReadOnly, AcceptedSuffixes: []string{".pptx"}}}, Prompt: model.InvocationPrompt{Instructions: "references/read.md", SkillBody: model.SkillBodyOmit}, ToolPolicy: model.ToolPolicy{Allow: []string{"run_skill_command"}, Required: []string{"run_skill_command"}}, Result: model.ResultContract{Kind: model.ResultKindMessage}},
			{ID: "work", Handle: "demo", Description: "Create demo documents", AgentMode: model.AgentModeSpec{Mode: model.AgentModeFork}, RuntimeProfile: "work", Request: model.RequestContract{Task: model.TaskContract{Required: true}, Inputs: model.InputContract{MaxItems: 1, Access: model.InputAccessReadOnly}}, Prompt: model.InvocationPrompt{Instructions: "references/work.md", SkillBody: model.SkillBodyInclude}, ToolPolicy: model.ToolPolicy{Allow: []string{"run_skill_command", "view_image"}, Required: []string{"run_skill_command"}}, Result: model.ResultContract{Kind: model.ResultKindMessage}},
		},
	}
}
