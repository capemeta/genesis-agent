package collision

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
)

type fakeCatalogService struct {
	entries []skillmodel.Metadata
}

func (f fakeCatalogService) Catalog(context.Context, skillcontract.CatalogRequest) (skillmodel.Catalog, error) {
	return skillmodel.Catalog{Entries: f.entries}, nil
}

func (f fakeCatalogService) Resolve(context.Context, skillcontract.ResolveRequest) (skillmodel.Metadata, error) {
	return skillmodel.Metadata{}, nil
}

func (f fakeCatalogService) Load(context.Context, skillcontract.LoadRequest) (skillmodel.Injection, error) {
	return skillmodel.Injection{}, nil
}

func (f fakeCatalogService) SelectForTurn(context.Context, skillcontract.SelectionRequest) ([]skillmodel.Metadata, error) {
	return nil, nil
}

func (f fakeCatalogService) RenderAvailableSkills(context.Context, skillcontract.CatalogRequest) (string, error) {
	return "", nil
}

func (f fakeCatalogService) ListResources(context.Context, skillcontract.ListResourcesRequest) (skillmodel.ResourceList, error) {
	return skillmodel.ResourceList{}, nil
}

func (f fakeCatalogService) ReadResource(context.Context, skillcontract.ResourceRequest) (skillmodel.ResourceContent, error) {
	return skillmodel.ResourceContent{}, nil
}

func (f fakeCatalogService) SearchResources(context.Context, skillcontract.SearchResourcesRequest) (skillmodel.SearchResult, error) {
	return skillmodel.SearchResult{}, nil
}

func (f fakeCatalogService) ClearCache() {}

func TestMatcherHitsSkillName(t *testing.T) {
	m := &Matcher{
		Service: fakeCatalogService{entries: []skillmodel.Metadata{
			{Name: "office-ppt", QualifiedName: "office-ppt"},
		}},
	}
	canonical, ok, err := m.Match(context.Background(), "office-ppt")
	if err != nil || !ok || canonical != "office-ppt" {
		t.Fatalf("Match = (%q,%v,%v)", canonical, ok, err)
	}
}

func TestMatcherIgnoresGatewayNames(t *testing.T) {
	m := &Matcher{
		Service: fakeCatalogService{entries: []skillmodel.Metadata{
			{Name: "office-ppt", QualifiedName: "office-ppt"},
		}},
	}
	for _, name := range []string{"Skill", ""} {
		if _, ok, err := m.Match(context.Background(), name); err != nil || ok {
			t.Fatalf("Match(%q) should miss, got ok=%v err=%v", name, ok, err)
		}
	}
}

func TestFormatResultIsStructuredCollision(t *testing.T) {
	raw := FormatResult("office-ppt", "office-ppt")
	var got Result
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "skill_tool_collision" || got.SuggestedTool != "Skill" {
		t.Fatalf("got = %+v", got)
	}
	if got.SuggestedArgs["skill"] != "office-ppt" {
		t.Fatalf("suggested_args = %#v", got.SuggestedArgs)
	}
	if !strings.Contains(got.Message, `Skill(skill="office-ppt")`) {
		t.Fatalf("message = %q", got.Message)
	}
}
