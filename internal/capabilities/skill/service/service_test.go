package service

import (
	"context"
	"strings"
	"testing"
	"time"

	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

func TestServiceResolveLoadAndRender(t *testing.T) {
	source := fakeSource{meta: model.Metadata{Name: "review", QualifiedName: "review", Description: "Review things", Enabled: true, PromptVisible: true, Authority: model.Authority{Kind: model.SourceKindHost, ID: "fake"}, PackageID: "review", MainResource: "review/SKILL.md"}.Normalize(), body: "Use careful review with a deliberately long body that should be truncated by the prompt budget."}
	svc := New([]contract.Source{source}, Options{MaxPromptBytes: 60, MaxListBytes: 200})
	req := contract.CatalogRequest{Product: profilemodel.ChannelCLI, Environment: profilemodel.EnvironmentLocal}
	catalog, err := svc.Catalog(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Entries) != 1 {
		t.Fatalf("entries=%d", len(catalog.Entries))
	}
	injection, err := svc.Load(context.Background(), contract.LoadRequest{ResolveRequest: contract.ResolveRequest{CatalogRequest: req, Name: "review", ModelCall: true}, Args: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if !injection.Truncated || !strings.Contains(injection.Contents, "[skill内容已按预算截断]") {
		t.Fatalf("expected truncated injection: %+v", injection)
	}
	rendered, err := svc.RenderAvailableSkills(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "review") {
		t.Fatalf("rendered = %q", rendered)
	}
}

type fakeSource struct {
	meta      model.Metadata
	body      string
	resources map[model.ResourceID]string
}

func (f fakeSource) Authority() model.Authority { return f.meta.Authority }
func (f fakeSource) List(context.Context, contract.ListQuery) (contract.ListResult, error) {
	return contract.ListResult{Entries: []model.Metadata{f.meta}}, nil
}
func (f fakeSource) Read(_ context.Context, req contract.ReadRequest) (contract.ReadResult, error) {
	if req.Resource != "" && req.Resource != f.meta.MainResource {
		return contract.ReadResult{Metadata: f.meta, Resource: req.Resource, Content: f.resources[req.Resource]}, nil
	}
	return contract.ReadResult{Metadata: f.meta, Resource: f.meta.MainResource, Content: f.body}, nil
}
func (f fakeSource) Search(context.Context, contract.SearchRequest) (contract.SearchResult, error) {
	matches := make([]model.SearchMatch, 0, len(f.resources))
	for resource := range f.resources {
		matches = append(matches, model.SearchMatch{Resource: resource, Title: string(resource), Snippet: f.resources[resource]})
	}
	return contract.SearchResult{Matches: matches}, nil
}

func TestServiceReadAndSearchResources(t *testing.T) {
	meta := model.Metadata{Name: "review", QualifiedName: "review", Description: "Review things", Enabled: true, PromptVisible: true, Authority: model.Authority{Kind: model.SourceKindHost, ID: "fake"}, PackageID: "review", MainResource: "review/SKILL.md"}.Normalize()
	source := fakeSource{meta: meta, body: "Body", resources: map[model.ResourceID]string{"review/references/guide.md": "alpha beta"}}
	svc := New([]contract.Source{source}, Options{})
	req := contract.CatalogRequest{Product: profilemodel.ChannelCLI, Environment: profilemodel.EnvironmentLocal}
	resource, err := svc.ReadResource(context.Background(), contract.ResourceRequest{ResolveRequest: contract.ResolveRequest{CatalogRequest: req, Name: "review"}, Resource: "review/references/guide.md"})
	if err != nil {
		t.Fatal(err)
	}
	if resource.Content != "alpha beta" {
		t.Fatalf("resource = %+v", resource)
	}
	matches, err := svc.SearchResources(context.Background(), contract.SearchResourcesRequest{ResolveRequest: contract.ResolveRequest{CatalogRequest: req, Name: "review"}, Query: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches.Matches) != 1 {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestServiceSelectForTurnSkillURIAndAmbiguousName(t *testing.T) {
	metaA := model.Metadata{Name: "review", QualifiedName: "a:review", Description: "Review A", Enabled: true, PromptVisible: true, Authority: model.Authority{Kind: model.SourceKindHost, ID: "a"}, PackageID: "review-a", MainResource: "review-a/SKILL.md"}.Normalize()
	metaB := model.Metadata{Name: "review", QualifiedName: "b:review", Description: "Review B", Enabled: true, PromptVisible: true, Authority: model.Authority{Kind: model.SourceKindHost, ID: "b"}, PackageID: "review-b", MainResource: "review-b/SKILL.md"}.Normalize()
	svc := New([]contract.Source{fakeSource{meta: metaA, body: "A"}, fakeSource{meta: metaB, body: "B"}}, Options{})
	req := contract.SelectionRequest{CatalogRequest: contract.CatalogRequest{Product: profilemodel.ChannelCLI, Environment: profilemodel.EnvironmentLocal}, Text: "use $review and [$review](skill://review-b/SKILL.md)"}
	selected, err := svc.SelectForTurn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].PackageID != "review-b" {
		t.Fatalf("selected = %+v", selected)
	}
}

func TestServiceSourceTimeoutKeepsOtherSources(t *testing.T) {
	fast := fakeSource{meta: model.Metadata{Name: "fast", QualifiedName: "fast", Description: "Fast", Enabled: true, PromptVisible: true, Authority: model.Authority{Kind: model.SourceKindHost, ID: "fast"}, PackageID: "fast", MainResource: "fast/SKILL.md"}.Normalize(), body: "fast"}
	svc := New([]contract.Source{slowSource{authority: model.Authority{Kind: model.SourceKindHost, ID: "slow"}}, fast}, Options{SourceTimeout: 50 * time.Millisecond})
	catalog, err := svc.Catalog(context.Background(), contract.CatalogRequest{Product: profilemodel.ChannelCLI, Environment: profilemodel.EnvironmentLocal})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Entries) != 1 || catalog.Entries[0].Name != "fast" {
		t.Fatalf("catalog = %+v", catalog)
	}
	if len(catalog.Errors) == 0 {
		t.Fatal("expected slow source error")
	}
}

type slowSource struct{ authority model.Authority }

func (s slowSource) Authority() model.Authority { return s.authority }
func (s slowSource) List(ctx context.Context, query contract.ListQuery) (contract.ListResult, error) {
	<-ctx.Done()
	return contract.ListResult{}, ctx.Err()
}
func (s slowSource) Read(context.Context, contract.ReadRequest) (contract.ReadResult, error) {
	return contract.ReadResult{}, nil
}
func (s slowSource) Search(context.Context, contract.SearchRequest) (contract.SearchResult, error) {
	return contract.SearchResult{}, nil
}

func TestServiceDoesNotCachePartialCatalogWithErrors(t *testing.T) {
	flaky := &flakySource{meta: model.Metadata{Name: "flaky", QualifiedName: "flaky", Description: "Flaky", Enabled: true, PromptVisible: true, Authority: model.Authority{Kind: model.SourceKindHost, ID: "flaky"}, PackageID: "flaky", MainResource: "flaky/SKILL.md"}.Normalize()}
	svc := New([]contract.Source{flaky}, Options{})
	req := contract.CatalogRequest{Product: profilemodel.ChannelCLI, Environment: profilemodel.EnvironmentLocal}
	first, err := svc.Catalog(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Errors) == 0 {
		t.Fatal("expected first catalog error")
	}
	second, err := svc.Catalog(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Errors) != 0 || len(second.Entries) != 1 {
		t.Fatalf("second catalog = %+v", second)
	}
}

type flakySource struct {
	meta  model.Metadata
	calls int
}

func (f *flakySource) Authority() model.Authority { return f.meta.Authority }
func (f *flakySource) List(context.Context, contract.ListQuery) (contract.ListResult, error) {
	f.calls++
	if f.calls == 1 {
		return contract.ListResult{}, context.DeadlineExceeded
	}
	return contract.ListResult{Entries: []model.Metadata{f.meta}}, nil
}
func (f *flakySource) Read(context.Context, contract.ReadRequest) (contract.ReadResult, error) {
	return contract.ReadResult{Metadata: f.meta, Resource: f.meta.MainResource, Content: "body"}, nil
}
func (f *flakySource) Search(context.Context, contract.SearchRequest) (contract.SearchResult, error) {
	return contract.SearchResult{}, nil
}
