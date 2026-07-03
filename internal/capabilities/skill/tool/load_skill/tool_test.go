package load_skill

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
)

func TestLoadSkillAllowsAvailableToolDependency(t *testing.T) {
	tool := newTestTool(t, skillmodel.Dependencies{Tools: []skillmodel.ToolDependency{{Type: "tool", Value: "read_file"}}}, []string{"read_file"})
	out, err := tool.Execute(context.Background(), `{"name":"review"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"dependencies"`) || !strings.Contains(out, `"status":"available"`) {
		t.Fatalf("output = %s", out)
	}
}

func TestLoadSkillRejectsMissingToolDependency(t *testing.T) {
	tool := newTestTool(t, skillmodel.Dependencies{Tools: []skillmodel.ToolDependency{{Type: "tool", Value: "grep"}}}, []string{"read_file"})
	_, err := tool.Execute(context.Background(), `{"name":"review"}`)
	if err == nil || !strings.Contains(err.Error(), "依赖未启用工具") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadSkillAsksForExternalDependency(t *testing.T) {
	tool := newTestTool(t, skillmodel.Dependencies{Tools: []skillmodel.ToolDependency{{Type: "mcp", Value: "github"}}}, []string{"read_file"})
	_, err := tool.Execute(context.Background(), `{"name":"review"}`)
	if err == nil || !strings.Contains(err.Error(), "依赖未通过审批") {
		t.Fatalf("err = %v", err)
	}
}

func newTestTool(t *testing.T, deps skillmodel.Dependencies, enabledTools []string) *Tool {
	t.Helper()
	meta := skillmodel.Metadata{Name: "review", QualifiedName: "review", Description: "Review", Enabled: true, PromptVisible: true, Authority: skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, PackageID: "review", MainResource: "review/SKILL.md", Dependencies: deps}.Normalize()
	source := skillmemory.NewSource(meta.Authority, []skillmemory.Skill{{Metadata: meta, Body: "Body"}})
	svc := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	approval, err := approvalservice.New(approvalstatic.NewPolicyEngine(), approvaldeny.NewRequester(), approvalmemory.NewStore())
	if err != nil {
		t.Fatal(err)
	}
	created, err := New(Deps{Service: svc, Approval: approval, EnabledTools: enabledTools})
	if err != nil {
		t.Fatal(err)
	}
	return created.(*Tool)
}
