package skill

import (
	"context"
	"strings"
	"testing"

	approvaldeny "genesis-agent/internal/capabilities/approval/adapter/deny"
	approvalmemory "genesis-agent/internal/capabilities/approval/adapter/memory"
	approvalstatic "genesis-agent/internal/capabilities/approval/adapter/static"
	approvalservice "genesis-agent/internal/capabilities/approval/service"
	skillmemory "genesis-agent/internal/capabilities/skill/adapter/memory"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	"genesis-agent/internal/platform/logger"
)

func TestSkillAllowsAvailableToolDependency(t *testing.T) {
	tool := newTestTool(t, skillmodel.Dependencies{Tools: []skillmodel.ToolDependency{{Type: "tool", Value: "read_file"}}}, []string{"read_file"})
	out, err := tool.Execute(context.Background(), `{"skill":"review"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"dependencies"`) || !strings.Contains(out, `"status":"available"`) {
		t.Fatalf("output = %s", out)
	}
}

func TestSkillAcceptsSkillParam(t *testing.T) {
	tool := newTestTool(t, skillmodel.Dependencies{}, []string{"read_file"})
	out, err := tool.Execute(context.Background(), `{"skill":"review"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"type":"skill_injection"`) || !strings.Contains(out, `"name":"review"`) {
		t.Fatalf("output = %s", out)
	}
}

func TestSkillRejectsLegacyNameParam(t *testing.T) {
	tool := newTestTool(t, skillmodel.Dependencies{}, []string{"read_file"})
	_, err := tool.Execute(context.Background(), `{"name":"review"}`)
	if err == nil || !strings.Contains(err.Error(), "skill或resource") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkillToolExposesGatewayNameAndDescriptionFunc(t *testing.T) {
	tool := newTestTool(t, skillmodel.Dependencies{}, []string{"read_file"})
	info := tool.GetInfo()
	if info.Name != "Skill" {
		t.Fatalf("Name = %q, want Skill", info.Name)
	}
	if _, ok := info.Parameters.Properties["name"]; ok {
		t.Fatal("legacy name parameter should be removed")
	}
	if info.DescriptionFunc == nil {
		t.Fatal("DescriptionFunc is nil")
	}
	desc, err := info.DescriptionFunc(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(desc, "<available_skills>") || !strings.Contains(desc, "review") {
		t.Fatalf("description = %q", desc)
	}
}

func TestSkillExplicitLoadAllowsDisableModelInvocation(t *testing.T) {
	meta := skillmodel.Metadata{
		Name: "manual", QualifiedName: "manual", Description: "Manual", Enabled: true, PromptVisible: true,
		Authority: skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, PackageID: "manual", MainResource: "manual/SKILL.md",
		Policy: skillmodel.Policy{DisableModelInvocation: true},
	}.Normalize()
	source := skillmemory.NewSource(meta.Authority, []skillmemory.Skill{{Metadata: meta, Body: "Manual body"}})
	svc := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	approval, err := approvalservice.New(approvalstatic.NewPolicyEngine(), approvaldeny.NewRequester(), approvalmemory.NewStore(), logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	created, err := New(Deps{Service: svc, Approval: approval, EnabledTools: []string{"read_file"}})
	if err != nil {
		t.Fatal(err)
	}
	gateway := created.(*Tool)
	if _, err := gateway.Execute(context.Background(), `{"skill":"manual"}`); err == nil {
		t.Fatal("model path should reject manual-only skill")
	}
	out, err := gateway.LoadExplicitSkill(context.Background(), skillcontract.ExplicitLoadRequest{Skill: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"type":"skill_injection"`) || !strings.Contains(out, "Manual body") {
		t.Fatalf("output = %s", out)
	}
}
func TestSkillRejectsMissingToolDependency(t *testing.T) {
	tool := newTestTool(t, skillmodel.Dependencies{Tools: []skillmodel.ToolDependency{{Type: "tool", Value: "grep"}}}, []string{"read_file"})
	_, err := tool.Execute(context.Background(), `{"skill":"review"}`)
	if err == nil || !strings.Contains(err.Error(), "依赖未启用工具") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkillAsksForExternalDependency(t *testing.T) {
	tool := newTestTool(t, skillmodel.Dependencies{Tools: []skillmodel.ToolDependency{{Type: "mcp", Value: "github"}}}, []string{"read_file"})
	_, err := tool.Execute(context.Background(), `{"skill":"review"}`)
	if err == nil || !strings.Contains(err.Error(), "依赖未通过审批") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkillRejectsForkContext(t *testing.T) {
	meta := skillmodel.Metadata{
		Name: "forked", QualifiedName: "forked", Description: "Forked", Enabled: true, PromptVisible: true,
		Authority: skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, PackageID: "forked",
		MainResource: "forked/SKILL.md", Context: skillmodel.ContextModeFork,
	}.Normalize()
	source := skillmemory.NewSource(meta.Authority, []skillmemory.Skill{{Metadata: meta, Body: "Body"}})
	svc := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	approval, err := approvalservice.New(approvalstatic.NewPolicyEngine(), approvaldeny.NewRequester(), approvalmemory.NewStore(), logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	created, err := New(Deps{Service: svc, Approval: approval, EnabledTools: []string{"read_file"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = created.Execute(context.Background(), `{"skill":"forked"}`)
	if err == nil || !strings.Contains(err.Error(), "fork") {
		t.Fatalf("err = %v", err)
	}
}

func newTestTool(t *testing.T, deps skillmodel.Dependencies, enabledTools []string) *Tool {
	t.Helper()
	meta := skillmodel.Metadata{Name: "review", QualifiedName: "review", Description: "Review", Enabled: true, PromptVisible: true, Authority: skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, PackageID: "review", MainResource: "review/SKILL.md", Dependencies: deps}.Normalize()
	source := skillmemory.NewSource(meta.Authority, []skillmemory.Skill{{Metadata: meta, Body: "Body"}})
	svc := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	approval, err := approvalservice.New(approvalstatic.NewPolicyEngine(), approvaldeny.NewRequester(), approvalmemory.NewStore(), logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	created, err := New(Deps{Service: svc, Approval: approval, EnabledTools: enabledTools})
	if err != nil {
		t.Fatal(err)
	}
	return created.(*Tool)
}
