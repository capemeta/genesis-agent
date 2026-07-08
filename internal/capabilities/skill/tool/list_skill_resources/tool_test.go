package list_skill_resources

import (
	"context"
	"strings"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

type fakeSkillService struct {
	meta      model.Metadata
	resources []model.ResourceInfo
}

func (s fakeSkillService) Catalog(context.Context, skillcontract.CatalogRequest) (model.Catalog, error) {
	return model.Catalog{Entries: []model.Metadata{s.meta}}, nil
}
func (s fakeSkillService) Resolve(context.Context, skillcontract.ResolveRequest) (model.Metadata, error) {
	return s.meta, nil
}
func (s fakeSkillService) Load(context.Context, skillcontract.LoadRequest) (model.Injection, error) {
	return model.Injection{}, nil
}
func (s fakeSkillService) ReadResource(context.Context, skillcontract.ResourceRequest) (model.ResourceContent, error) {
	return model.ResourceContent{}, nil
}
func (s fakeSkillService) ListResources(context.Context, skillcontract.ListResourcesRequest) (model.ResourceList, error) {
	return model.ResourceList{Skill: s.meta, Resources: s.resources}, nil
}
func (s fakeSkillService) SearchResources(context.Context, skillcontract.SearchResourcesRequest) (model.SearchResult, error) {
	return model.SearchResult{}, nil
}
func (s fakeSkillService) SelectForTurn(context.Context, skillcontract.SelectionRequest) ([]model.Metadata, error) {
	return nil, nil
}
func (s fakeSkillService) RenderAvailableSkills(context.Context, skillcontract.CatalogRequest) (string, error) {
	return "", nil
}
func (s fakeSkillService) ClearCache() {}

type allowApproval struct{}

func (allowApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

func TestToolListsSkillResources(t *testing.T) {
	meta := model.Metadata{Name: "review", QualifiedName: "review", Description: "Review", Enabled: true, PromptVisible: true, Authority: model.Authority{Kind: model.SourceKindEmbedded, ID: "test"}, PackageID: "review", MainResource: "review/SKILL.md"}.Normalize()
	created, err := New(Deps{Service: fakeSkillService{meta: meta, resources: []model.ResourceInfo{{Resource: "review/assets/logo.bin", Kind: model.ResourceKindAsset, Name: "logo.bin", Size: 3, Text: false}, {Resource: "review/references/guide.md", Kind: model.ResourceKindReference, Name: "guide.md", Size: 10, Text: true}}}, Approval: allowApproval{}, CatalogRequest: skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := created.Execute(context.Background(), `{"name":"review"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"review/assets/logo.bin"`) || !strings.Contains(out, `"text":false`) || strings.Contains(out, "alpha beta") {
		t.Fatalf("output = %s", out)
	}
}
