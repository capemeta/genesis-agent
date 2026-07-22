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
	entries []skillmodel.InvocationMetadata
}

func (f fakeCatalogService) Catalog(context.Context, skillcontract.CatalogRequest) (skillmodel.Catalog, error) {
	return skillmodel.Catalog{Entries: f.entries}, nil
}

func TestMatcherHitsSkillName(t *testing.T) {
	m := &Matcher{
		Service: fakeCatalogService{entries: []skillmodel.InvocationMetadata{
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
		Service: fakeCatalogService{entries: []skillmodel.InvocationMetadata{
			{Name: "office-ppt", QualifiedName: "office-ppt"},
		}},
	}
	for _, name := range []string{"Skill", ""} {
		if _, ok, err := m.Match(context.Background(), name); err != nil || ok {
			t.Fatalf("Match(%q) should miss, got ok=%v err=%v", name, ok, err)
		}
	}
}

func TestRewriteArgsUsesOnlyPublicTaskParameter(t *testing.T) {
	var got map[string]string
	if err := json.Unmarshal([]byte(RewriteArgs("office-ppt", "生成季度汇报")), &got); err != nil {
		t.Fatal(err)
	}
	if got["skill"] != "office-ppt" || got["task"] != "生成季度汇报" || got["args"] != "" {
		t.Fatalf("got=%v", got)
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
