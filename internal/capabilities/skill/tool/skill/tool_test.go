package skill

import (
	"context"
	"fmt"
	"strings"
	"testing"

	approvaldeny "genesis-agent/internal/capabilities/approval/adapter/deny"
	approvalmemory "genesis-agent/internal/capabilities/approval/adapter/memory"
	approvalstatic "genesis-agent/internal/capabilities/approval/adapter/static"
	approvalservice "genesis-agent/internal/capabilities/approval/service"
	"genesis-agent/internal/capabilities/llm/vision"
	viewimage "genesis-agent/internal/capabilities/media/tool/view_image"
	skillmemory "genesis-agent/internal/capabilities/skill/adapter/memory"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	subagentcontract "genesis-agent/internal/capabilities/subagent/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
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

func TestSkillForkRequiresTaskGateway(t *testing.T) {
	meta := skillmodel.Metadata{
		Name: "forked", QualifiedName: "forked", Description: "Forked", Enabled: true, PromptVisible: true,
		Authority: skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, PackageID: "forked",
		MainResource: "forked/SKILL.md", Context: skillmodel.ContextModeFork, AllowedTools: []string{"read_file"},
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

type recordingForkTask struct{ last subagentcontract.DelegateRequest }

func (t *recordingForkTask) GetInfo() *tool.Info { return &tool.Info{Name: "Task"} }
func (t *recordingForkTask) Execute(context.Context, string) (string, error) {
	return "", fmt.Errorf("Skill fork 必须走 Delegator.Delegate")
}
func (t *recordingForkTask) Delegate(_ context.Context, req subagentcontract.DelegateRequest) (string, error) {
	t.last = req
	return `{"status":"completed","agent_id":"agent-1","summary":"done"}`, nil
}

func TestSkillForkDelegatesThroughTaskGateway(t *testing.T) {
	meta := skillmodel.Metadata{
		Name: "forked", QualifiedName: "forked", Description: "Forked", Enabled: true, PromptVisible: true,
		Authority: skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, PackageID: "forked",
		MainResource: "forked/SKILL.md", Context: skillmodel.ContextModeFork, AllowedTools: []string{"read_file"},
	}.Normalize()
	source := skillmemory.NewSource(meta.Authority, []skillmemory.Skill{{Metadata: meta, Body: "Skill body"}})
	svc := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	approval, err := approvalservice.New(approvalstatic.NewPolicyEngine(), approvaldeny.NewRequester(), approvalmemory.NewStore(), logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	created, err := New(Deps{Service: svc, Approval: approval, EnabledTools: []string{"read_file"}})
	if err != nil {
		t.Fatal(err)
	}
	gateway := &recordingForkTask{}
	created.(*Tool).SetForkTask(gateway)
	out, err := created.Execute(context.Background(), `{"skill":"forked","args":"inspect config"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"agent_id":"agent-1"`) {
		t.Fatalf("fork output missing agent_id: %q", out)
	}
	if gateway.last.Definition == nil || gateway.last.Definition.Name != "skill-fork:forked" {
		t.Fatalf("expected synthetic skill-fork definition, got %#v", gateway.last.Definition)
	}
	if gateway.last.Definition.SystemPrompt != "Skill body" {
		t.Fatalf("skill body should be definition system: %#v", gateway.last.Definition)
	}
	if gateway.last.Prompt != "inspect config" || gateway.last.SnapshotMode != subagentcontract.SnapshotModeSkillIsolated {
		t.Fatalf("unexpected fork request: %#v", gateway.last)
	}
	if len(gateway.last.AllowedTools) != 1 || gateway.last.AllowedTools[0] != "read_file" {
		t.Fatalf("unexpected allowed tools: %#v", gateway.last.AllowedTools)
	}
}

func TestSkillForkNamedAgentKeepsSkillIsolated(t *testing.T) {
	meta := skillmodel.Metadata{
		Name: "forked", QualifiedName: "forked", Description: "Forked", Enabled: true, PromptVisible: true,
		Authority: skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, PackageID: "forked",
		MainResource: "forked/SKILL.md", Context: skillmodel.ContextModeFork, Agent: "explore", AllowedTools: []string{"read_file"},
	}.Normalize()
	source := skillmemory.NewSource(meta.Authority, []skillmemory.Skill{{Metadata: meta, Body: "Skill body"}})
	svc := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	approval, err := approvalservice.New(approvalstatic.NewPolicyEngine(), approvaldeny.NewRequester(), approvalmemory.NewStore(), logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	created, err := New(Deps{Service: svc, Approval: approval, EnabledTools: []string{"read_file"}})
	if err != nil {
		t.Fatal(err)
	}
	gateway := &recordingForkTask{}
	created.(*Tool).SetForkTask(gateway)
	if _, err := created.Execute(context.Background(), `{"skill":"forked","args":"inspect"}`); err != nil {
		t.Fatal(err)
	}
	if gateway.last.SubagentType != "explore" || gateway.last.Definition != nil {
		t.Fatalf("named agent must use Catalog type: %#v", gateway.last)
	}
	if gateway.last.SnapshotMode != subagentcontract.SnapshotModeSkillIsolated {
		t.Fatalf("named skill fork must stay skill_isolated: %#v", gateway.last)
	}
	if !strings.Contains(gateway.last.Prompt, "Skill body") || !strings.Contains(gateway.last.Prompt, "inspect") {
		t.Fatalf("prompt should carry body + args: %#v", gateway.last.Prompt)
	}
}

func TestSkillRejectsRequiredVisionWithoutCapability(t *testing.T) {
	meta := skillmodel.Metadata{
		Name: "vision-review", QualifiedName: "vision-review", Description: "Need vision",
		Enabled: true, PromptVisible: true,
		Authority: skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"},
		PackageID: "vision-review", MainResource: "vision-review/SKILL.md",
		Requires: []skillmodel.CapabilityRequirement{{Kind: "vision", Enforcement: "required"}},
	}.Normalize()
	source := skillmemory.NewSource(meta.Authority, []skillmemory.Skill{{Metadata: meta, Body: "Body"}})
	svc := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	approval, err := approvalservice.New(approvalstatic.NewPolicyEngine(), approvaldeny.NewRequester(), approvalmemory.NewStore(), logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	created, err := New(Deps{Service: svc, Approval: approval, EnabledTools: []string{"view_image"}})
	if err != nil {
		t.Fatal(err)
	}
	// 无 VisionMode 注入 → 视为 degraded_text
	if _, err := created.Execute(context.Background(), `{"skill":"vision-review"}`); err == nil || !strings.Contains(err.Error(), "SKILL_CAPABILITY_REQUIRED") {
		t.Fatalf("expected vision required failure, got %v", err)
	}
	ctx := viewimage.WithVisionMode(context.Background(), vision.ModeDirectInject)
	out, err := created.Execute(ctx, `{"skill":"vision-review"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"type":"skill_injection"`) {
		t.Fatalf("with vision capability should load: %s", out)
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

func TestSkillForkAcceptsExplicitInputs(t *testing.T) {
	meta := skillmodel.Metadata{
		Name: "forked", QualifiedName: "forked", Description: "Forked", Enabled: true, PromptVisible: true,
		Authority: skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}, PackageID: "forked",
		MainResource: "forked/SKILL.md", Context: skillmodel.ContextModeFork, AllowedTools: []string{"read_file"},
	}.Normalize()
	source := skillmemory.NewSource(meta.Authority, []skillmemory.Skill{{Metadata: meta, Body: "Skill body"}})
	svc := skillservice.New([]skillcontract.Source{source}, skillservice.Options{})
	approval, err := approvalservice.New(approvalstatic.NewPolicyEngine(), approvaldeny.NewRequester(), approvalmemory.NewStore(), logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	created, err := New(Deps{Service: svc, Approval: approval, EnabledTools: []string{"read_file"}})
	if err != nil {
		t.Fatal(err)
	}
	gateway := &recordingForkTask{}
	created.(*Tool).SetForkTask(gateway)

	// Explicit inputs parameter passed by LLM
	_, err = created.Execute(context.Background(), `{"skill":"forked","args":"create ppt","inputs":["explicit_doc.md"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(gateway.last.InputFiles) != 1 || gateway.last.InputFiles[0] != "explicit_doc.md" {
		t.Fatalf("expected explicit input file [explicit_doc.md], got %#v", gateway.last.InputFiles)
	}
}

func TestExtractWorkspaceInputFilesIntersection(t *testing.T) {
	text := "根据ultra5-comparison-summary.md创建PPT，主题色红色，文件名2026笔记本选型比较.pptx"
	inputs := extractFallbackInputFiles(text)
	if len(inputs) == 0 {
		t.Fatalf("expected fallback input extraction, got empty")
	}
}
