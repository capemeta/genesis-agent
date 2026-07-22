package read_skill_resource

import (
	"context"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

type capturingSkillService struct {
	request      skillcontract.ResourceRequest
	boundRequest skillcontract.BoundResourceRequest
}

func (s *capturingSkillService) Resolve(context.Context, skillcontract.ResolveRequest) (model.ResolvedInvocation, error) {
	return model.ResolvedInvocation{Definition: model.InvocationDefinition{AgentMode: model.AgentModeSpec{Mode: model.AgentModeMain}}}, nil
}

func (s *capturingSkillService) ReadResource(_ context.Context, req skillcontract.ResourceRequest) (model.ResourceContent, error) {
	s.request = req
	return model.ResourceContent{
		Skill:    model.Metadata{Name: req.Name},
		Resource: req.Resource,
		Content:  "guide",
	}, nil
}

func (s *capturingSkillService) ReadBoundResource(_ context.Context, req skillcontract.BoundResourceRequest) (model.ResourceContent, error) {
	s.boundRequest = req
	return model.ResourceContent{Skill: model.Metadata{Name: req.Binding.PhysicalSkill}, Resource: req.Resource, Content: "guide"}, nil
}

type allowApproval struct{}

func (allowApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

func TestExecuteAcceptsSkill(t *testing.T) {
	service := &capturingSkillService{}
	created, err := New(Deps{Service: service, Approval: allowApproval{}, CatalogRequest: skillcontract.CatalogRequest{Product: profilemodel.ChannelCLI}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.Execute(context.Background(), `{"skill":"demo","resource":"design.md"}`); err != nil {
		t.Fatal(err)
	}
	if service.request.Name != "demo" || service.request.Resource != "demo/design.md" {
		t.Fatalf("request = %+v", service.request)
	}
}

func TestExecuteDerivesOwnerFromQualifiedResource(t *testing.T) {
	service := &capturingSkillService{}
	created, err := New(Deps{Service: service, Approval: allowApproval{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.Execute(context.Background(), `{"resource":"demo/references/guide.md"}`); err != nil {
		t.Fatal(err)
	}
	if service.request.Name != "demo" || service.request.Resource != "demo/references/guide.md" {
		t.Fatalf("request = %+v", service.request)
	}
}

func TestExecuteUsesBindingPhysicalPackageForInvocationHandle(t *testing.T) {
	service := &capturingSkillService{}
	created, err := New(Deps{Service: service, Approval: allowApproval{}})
	if err != nil {
		t.Fatal(err)
	}
	binding := model.InvocationBinding{ID: "binding-read", Handle: "office-ppt-read", PhysicalSkill: "office-ppt", Package: model.SkillPackageSnapshot{PackageID: "office-ppt"}}
	ctx := skillcontract.WithInvocationBinding(context.Background(), binding)
	if _, err := created.Execute(ctx, `{"skill":"office-ppt-read","resource":"references/invocations/read.md"}`); err != nil {
		t.Fatal(err)
	}
	if service.boundRequest.Resource != "office-ppt/references/invocations/read.md" {
		t.Fatalf("bound request = %+v", service.boundRequest)
	}
}

// TestExecuteRejectsLegacyNameParam 固化单一权威参数设计：技能标识只认 skill，
// 旧的 name 参数已移除，传入会作为未知字段被拒，避免 schema 里出现两个易混淆的名字。
func TestExecuteRejectsLegacyNameParam(t *testing.T) {
	created, err := New(Deps{Service: &capturingSkillService{}, Approval: allowApproval{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.Execute(context.Background(), `{"name":"demo","resource":"design.md"}`); err == nil {
		t.Fatal("expected legacy name param to be rejected")
	}
}
