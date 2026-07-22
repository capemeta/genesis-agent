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

func (s fakeSkillService) ListResources(context.Context, skillcontract.ListResourcesRequest) (model.ResourceList, error) {
	return model.ResourceList{Skill: s.meta, Resources: s.resources}, nil
}

func (s fakeSkillService) ListBoundResources(context.Context, model.InvocationBinding) (model.ResourceList, error) {
	return model.ResourceList{Skill: s.meta, Resources: s.resources}, nil
}

type allowApproval struct{}

func (allowApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

func TestToolListsSkillResources(t *testing.T) {
	meta := model.Metadata{Name: "review", Description: "Review", Authority: model.Authority{Kind: model.SourceKindEmbedded, ID: "test"}, PackageID: "review", MainResource: "review/SKILL.md"}.Normalize()
	created, err := New(Deps{Service: fakeSkillService{meta: meta, resources: []model.ResourceInfo{{Resource: "review/assets/logo.bin", Kind: model.ResourceKindAsset, Name: "logo.bin", Size: 3, Text: false}, {Resource: "review/references/guide.md", Kind: model.ResourceKindReference, Name: "guide.md", Size: 10, Text: true}}}, Approval: allowApproval{}, CatalogRequest: skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := created.Execute(context.Background(), `{"skill":"review"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"review/assets/logo.bin"`) || !strings.Contains(out, `"text":false`) || strings.Contains(out, "alpha beta") {
		t.Fatalf("output = %s", out)
	}
}

// TestToolRejectsLegacyNameParam 固化单一权威参数设计：技能标识只认 skill（与 Skill/run_skill_command
// 等全家族一致），旧的 name 参数已移除，传入会作为未知字段被拒，避免 schema 出现两个易混淆的名字。
func TestToolRejectsLegacyNameParam(t *testing.T) {
	meta := model.Metadata{Name: "review", Description: "Review", Authority: model.Authority{Kind: model.SourceKindEmbedded, ID: "test"}, PackageID: "review", MainResource: "review/SKILL.md"}.Normalize()
	deps := Deps{Service: fakeSkillService{meta: meta, resources: []model.ResourceInfo{{Resource: "review/references/guide.md", Kind: model.ResourceKindReference, Name: "guide.md", Size: 10, Text: true}}}, Approval: allowApproval{}, CatalogRequest: skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI}}
	created, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.Execute(context.Background(), `{"name":"review"}`); err == nil {
		t.Fatal("expected legacy name param to be rejected")
	}
}
