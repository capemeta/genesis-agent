package skill

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/capabilities/llm/vision"
	viewimage "genesis-agent/internal/capabilities/media/tool/view_image"
	skillmemory "genesis-agent/internal/capabilities/skill/adapter/memory"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	skillservice "genesis-agent/internal/capabilities/skill/service"
	subagentcontract "genesis-agent/internal/capabilities/subagent/contract"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/platform/contextutil"
)

type allowApproval struct{}

func (allowApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

type fixedInputs struct{}

func (fixedInputs) ResolveInputs(_ context.Context, inputs []string) ([]workmodel.ResourceRef, error) {
	out := make([]workmodel.ResourceRef, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, workmodel.ResourceRef{Authority: "host", Scheme: "run-input", ID: input, Version: "sha256:abc", Path: input})
	}
	return out, nil
}

type captureDelegator struct {
	request subagentcontract.DelegateRequest
}

func (d *captureDelegator) Delegate(_ context.Context, req subagentcontract.DelegateRequest) (string, error) {
	d.request = req
	return `{"status":"completed"}`, nil
}
func (d *captureDelegator) GetInfo() *toolcontract.Info {
	return &toolcontract.Info{Name: "Task", Parameters: &toolcontract.ParameterSchema{Type: "object"}}
}
func (d *captureDelegator) Execute(context.Context, string) (string, error) { return "", nil }

func TestSkillSchemaIsStableAndInlineBindingUsesOnlyPublicParameters(t *testing.T) {
	created := newInvocationTool(t, false)
	info := created.GetInfo()
	if info.Name != "Skill" || len(info.Parameters.Properties) != 3 {
		t.Fatalf("info=%+v", info)
	}
	for _, name := range []string{"skill", "task", "inputs"} {
		if info.Parameters.Properties[name] == nil {
			t.Fatalf("missing public parameter %s", name)
		}
	}
	if _, ok := info.Parameters.Properties["entrypoint"]; ok {
		t.Fatal("entrypoint must not enter model schema")
	}
	ctx := testContext(vision.ModeDegradedText)
	out, err := created.Execute(ctx, `{"skill":"demo-read","inputs":["deck.pptx"]}`)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["context"] != "main" || decoded["physical_skill"] != "demo" || decoded["binding_id"] == "" {
		t.Fatalf("output=%s", out)
	}
	if _, err := created.Execute(ctx, `{"skill":"demo-read","entrypoint":"read"}`); err == nil {
		t.Fatal("unknown model parameter must fail")
	}
}

func TestSkillForkDelegatesIsolatedBindingAndDeclarativeDeliverable(t *testing.T) {
	created := newInvocationTool(t, false)
	delegate := &captureDelegator{}
	created.SetForkTask(delegate)
	ctx := testContext(vision.ModeDirectInject)
	out, err := created.Execute(ctx, `{"skill":"demo","task":"生成项目汇报","inputs":["requirements.md"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"completed"`) {
		t.Fatalf("out=%s", out)
	}
	req := delegate.request
	if req.SnapshotMode != subagentcontract.SnapshotModeSkillIsolated || req.InvocationBinding.Handle != "demo" || req.InvocationBinding.RunID != "parent-run" {
		t.Fatalf("request=%+v", req)
	}
	if len(req.Deliverables) != 1 || req.Deliverables[0].ID != "deck" || req.Deliverables[0].QAPolicy != "visual-qa/v1" {
		t.Fatalf("deliverables=%+v", req.Deliverables)
	}
	if len(req.AllowedTools) != 6 || len(req.InputRefs) != 1 {
		t.Fatalf("tools=%v inputs=%v", req.AllowedTools, req.InputRefs)
	}
}

func TestSkillRequiredVisionChecksTargetBeforeFork(t *testing.T) {
	created := newInvocationTool(t, true)
	delegate := &captureDelegator{}
	created.SetForkTask(delegate)
	_, err := created.Execute(testContext(vision.ModeDegradedText), `{"skill":"demo","task":"生成"}`)
	if err == nil || !strings.Contains(err.Error(), "SKILL_CAPABILITY_REQUIRED") {
		t.Fatalf("err=%v", err)
	}
	if delegate.request.Prompt != "" {
		t.Fatal("capability gate must run before child creation")
	}
}

func TestResolveToolPolicyFailsClosed(t *testing.T) {
	if _, err := resolveToolPolicy([]string{"read_file"}, skillmodel.ToolPolicy{Allow: []string{"run_skill_command"}, Required: []string{"run_skill_command"}}); err == nil {
		t.Fatal("empty intersection must fail")
	}
	if _, err := resolveToolPolicy([]string{"run_skill_command"}, skillmodel.ToolPolicy{Allow: []string{"run_skill_command", "view_image"}, Required: []string{"view_image"}}); err == nil {
		t.Fatal("missing required tool must fail")
	}
}

func TestResolveExecutionPolicyPinsSafeDegradationInBindingFact(t *testing.T) {
	resolved := skillmodel.ResolvedInvocation{Definition: skillmodel.InvocationDefinition{Handle: "demo"}, Profile: skillmodel.RuntimeProfile{Sandbox: skillmodel.SandboxRequirement{ExecutionMode: skillmodel.ExecutionModeSandboxedSession, Backends: []string{"remote_sandbox", "local_platform_sandbox"}}}}
	tool := &Tool{deps: Deps{
		LocalSandboxAvailable: true, RemoteSandboxAvailable: false,
	}}
	policy, err := tool.resolveExecutionPolicy(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Degraded || policy.SelectedBackend != "local_platform_sandbox" || policy.PreferredBackend != "remote_sandbox" || len(policy.Warnings) != 1 {
		t.Fatalf("policy=%+v", policy)
	}
}

func TestResolveExecutionPolicyFailsClosedWhenBackendUnavailable(t *testing.T) {
	resolved := skillmodel.ResolvedInvocation{Definition: skillmodel.InvocationDefinition{Handle: "demo"}, Profile: skillmodel.RuntimeProfile{Sandbox: skillmodel.SandboxRequirement{ExecutionMode: skillmodel.ExecutionModePerCall, Backends: []string{"remote_sandbox"}}}}
	tool := &Tool{deps: Deps{LocalSandboxAvailable: true, RemoteSandboxAvailable: false}}
	if _, err := tool.resolveExecutionPolicy(resolved); err == nil || !strings.Contains(err.Error(), "SKILL_RUNTIME_PROFILE_UNAVAILABLE") {
		t.Fatalf("expected unavailable error, got %v", err)
	}
}

func newInvocationTool(t *testing.T, requiresVision bool) *Tool {
	t.Helper()
	authority := skillmodel.Authority{Kind: skillmodel.SourceKindEmbedded, ID: "test"}
	requires := []skillmodel.CapabilityRequirement(nil)
	if requiresVision {
		requires = []skillmodel.CapabilityRequirement{{Kind: "vision", Enforcement: "required"}}
	}
	manifest := skillmodel.RuntimeManifest{
		Schema: skillmodel.RuntimeManifestSchemaV1, Skill: "demo",
		RuntimeProfiles: map[string]skillmodel.RuntimeProfile{
			"read": {Sandbox: skillmodel.SandboxRequirement{ExecutionMode: skillmodel.ExecutionModePerCall, Backends: []string{"remote_sandbox", "local_platform_sandbox"}}},
			"work": {Sandbox: skillmodel.SandboxRequirement{ExecutionMode: skillmodel.ExecutionModeSandboxedSession, Backends: []string{"remote_sandbox", "local_platform_sandbox"}}},
		},
		Invocations: []skillmodel.InvocationDefinition{
			{ID: "read", Handle: "demo-read", Description: "Read demo", AgentMode: skillmodel.AgentModeSpec{Mode: skillmodel.AgentModeMain}, RuntimeProfile: "read", Request: skillmodel.RequestContract{Inputs: skillmodel.InputContract{MinItems: 1, MaxItems: 1, Access: skillmodel.InputAccessReadOnly, AcceptedSuffixes: []string{".pptx"}}}, Prompt: skillmodel.InvocationPrompt{SkillBody: skillmodel.SkillBodyOmit}, ToolPolicy: skillmodel.ToolPolicy{Allow: []string{"run_skill_command"}, Required: []string{"run_skill_command"}}, Result: skillmodel.ResultContract{Kind: skillmodel.ResultKindMessage}},
			{ID: "work", Handle: "demo", Description: "Create demo", AgentMode: skillmodel.AgentModeSpec{Mode: skillmodel.AgentModeFork}, RuntimeProfile: "work", Request: skillmodel.RequestContract{Task: skillmodel.TaskContract{Required: true}, Inputs: skillmodel.InputContract{MaxItems: 2, Access: skillmodel.InputAccessReadOnly}}, Prompt: skillmodel.InvocationPrompt{SkillBody: skillmodel.SkillBodyInclude}, ToolPolicy: skillmodel.ToolPolicy{Allow: []string{"run_skill_command", "view_image", "select_deliverable_candidate"}, Required: []string{"run_skill_command", "select_deliverable_candidate"}}, Requires: requires, Result: skillmodel.ResultContract{Kind: skillmodel.ResultKindDeliverables, Deliverables: []skillmodel.DeliverableDeclaration{{ID: "deck", Role: skillmodel.DeliverableRolePrimary, Required: true, Cardinality: skillmodel.DeliverableExactlyOne, AcceptedSuffixes: []string{".pptx"}, DeliveryPolicy: skillmodel.DeliveryPolicyRunOutput, QA: skillmodel.QADeclaration{Policy: "visual-qa/v1", Enforcement: "optional"}}}}},
		},
	}
	source := skillmemory.NewSource(authority, []skillmemory.Skill{{Metadata: skillmodel.Metadata{Name: "demo", Description: "Demo", Authority: authority, Scope: skillmodel.ScopeSystem, PackageID: "demo", MainResource: "demo/SKILL.md"}, Body: "portable instructions", Manifest: &manifest}})
	svc := skillservice.New([]skillcontract.Source{source}, skillservice.Options{KnownTools: []string{"run_skill_command", "view_image", "select_deliverable_candidate"}, BindingStore: skillmemory.NewBindingStore()})
	created, err := New(Deps{Service: svc, Approval: allowApproval{}, EnabledTools: []string{"run_skill_command", "view_image", "select_deliverable_candidate"}, InputResolver: fixedInputs{}, LocalSandboxAvailable: true, RemoteSandboxAvailable: true, PolicyVersion: "test/v1"})
	if err != nil {
		t.Fatal(err)
	}
	return created.(*Tool)
}

func testContext(mode vision.Mode) context.Context {
	ctx := contextutil.WithRunID(context.Background(), "parent-run")
	ctx = contextutil.WithTenantID(ctx, "tenant")
	return viewimage.WithVisionMode(ctx, mode)
}
