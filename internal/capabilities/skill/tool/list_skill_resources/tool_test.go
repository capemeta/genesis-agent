package list_skill_resources

import (
	"context"
	"strings"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
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

func (s fakeSkillService) Resolve(_ context.Context, req skillcontract.ResolveRequest) (model.ResolvedInvocation, error) {
	mode := model.AgentModeMain
	if req.Name == "office-ppt" || req.Name == "fork-skill" {
		mode = model.AgentModeFork
	}
	return model.ResolvedInvocation{Definition: model.InvocationDefinition{Handle: req.Name, AgentMode: model.AgentModeSpec{Mode: mode}}}, nil
}

func TestToolInjectsUsageNoticeForForkSkill(t *testing.T) {
	meta := model.Metadata{Name: "office-ppt", Description: "PPT", Authority: model.Authority{Kind: model.SourceKindEmbedded, ID: "test"}, PackageID: "office-ppt", MainResource: "office-ppt/SKILL.md"}.Normalize()
	deps := Deps{Service: fakeSkillService{meta: meta, resources: []model.ResourceInfo{{Resource: "office-ppt/references/design.md", Kind: model.ResourceKindReference, Name: "design.md", Size: 10, Text: true}}}, Approval: allowApproval{}, CatalogRequest: skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI}}
	created, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}

	manifest := workmodel.RunManifest{RunID: "run-master"}
	execution := workmodel.PreparedExecutionSnapshot{Binding: execmodel.ExecutionBinding{ID: "master-binding"}}
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: manifest, Execution: execution})

	out, err := created.Execute(ctx, `{"skill":"office-ppt"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"agent_mode":"fork"`) || !strings.Contains(out, `"usage_notice"`) || !strings.Contains(out, "主 Agent 禁止读取其包内资源文件") {
		t.Fatalf("output for fork skill missing usage_notice: %s", out)
	}
}
