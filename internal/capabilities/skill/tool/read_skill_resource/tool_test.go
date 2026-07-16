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
	request skillcontract.ResourceRequest
}

func (s *capturingSkillService) Catalog(context.Context, skillcontract.CatalogRequest) (model.Catalog, error) {
	return model.Catalog{}, nil
}
func (s *capturingSkillService) Resolve(context.Context, skillcontract.ResolveRequest) (model.Metadata, error) {
	return model.Metadata{}, nil
}
func (s *capturingSkillService) Load(context.Context, skillcontract.LoadRequest) (model.Injection, error) {
	return model.Injection{}, nil
}
func (s *capturingSkillService) ReadResource(_ context.Context, req skillcontract.ResourceRequest) (model.ResourceContent, error) {
	s.request = req
	return model.ResourceContent{
		Skill:    model.Metadata{Name: req.Name, QualifiedName: req.Name},
		Resource: req.Resource,
		Content:  "guide",
	}, nil
}
func (s *capturingSkillService) ListResources(context.Context, skillcontract.ListResourcesRequest) (model.ResourceList, error) {
	return model.ResourceList{}, nil
}
func (s *capturingSkillService) SearchResources(context.Context, skillcontract.SearchResourcesRequest) (model.SearchResult, error) {
	return model.SearchResult{}, nil
}
func (s *capturingSkillService) SelectForTurn(context.Context, skillcontract.SelectionRequest) ([]model.Metadata, error) {
	return nil, nil
}
func (s *capturingSkillService) RenderAvailableSkills(context.Context, skillcontract.CatalogRequest) (string, error) {
	return "", nil
}
func (s *capturingSkillService) ClearCache() {}

type allowApproval struct{}

func (allowApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

func TestExecuteAcceptsSkillAlias(t *testing.T) {
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

func TestExecuteRejectsConflictingNameAndSkill(t *testing.T) {
	created, err := New(Deps{Service: &capturingSkillService{}, Approval: allowApproval{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.Execute(context.Background(), `{"name":"one","skill":"two","resource":"guide.md"}`); err == nil {
		t.Fatal("expected conflict error")
	}
}
