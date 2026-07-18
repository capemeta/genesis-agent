package skillstack_test

import (
	"context"
	"testing"

	approvalmemory "genesis-agent/internal/capabilities/approval/adapter/memory"
	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	approvalservice "genesis-agent/internal/capabilities/approval/service"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	workspaceadapter "genesis-agent/internal/capabilities/workspace/adapter/sandbox"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/shared/skillstack"
)

type allowPolicy struct{}

func (allowPolicy) Evaluate(ctx context.Context, req approvalmodel.Request) (approvalmodel.PolicyResult, error) {
	return approvalmodel.PolicyResult{Type: approvalmodel.PolicyAllow, Reason: "test allow"}, nil
}

type denyRequester struct{}

func (denyRequester) RequestApproval(ctx context.Context, req approvalmodel.Request, result approvalmodel.PolicyResult) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionDenied, Reason: "should not ask"}, nil
}

type nopRunner struct{}

func (nopRunner) Run(ctx context.Context, cmd execmodel.Command, opts execcontract.RunOptions) (*execmodel.Result, error) {
	return &execmodel.Result{ExitCode: 0, Stdout: `{"ok":true}`}, nil
}

func TestBuildEmbeddedIncludesSharedOfficeWiring(t *testing.T) {
	approval, err := approvalservice.New(allowPolicy{}, denyRequester{}, approvalmemory.NewStore(), logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	stack, err := skillstack.BuildEmbedded(skillstack.Options{
		Product:     profilemodel.ChannelEnterprise,
		Environment: profilemodel.EnvironmentServer,
		Approval:    approval,
		Logger:      logger.NewNop(),
		StateRoot:   workmodel.StateRoot{ID: "test", Authority: "executor"},
		Provisioner: workspaceadapter.NewProvisioner(),
		EnabledTools: []string{
			"Skill", "run_skill_command", "install_skill_dependencies", "list_skill_resources", "read_skill_resource", "search_skill_resources",
		},
		Exec: skillstack.ExecStack{
			Runner:  nopRunner{},
			Sandbox: execmodel.SandboxProfile{Mode: execmodel.SandboxDisabled},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stack.Service == nil || len(stack.Tools) != 6 {
		t.Fatalf("stack=%+v", stack)
	}
	names := map[string]bool{}
	for _, tool := range stack.Tools {
		names[tool.GetInfo().Name] = true
	}
	for _, want := range []string{"Skill", "run_skill_command", "install_skill_dependencies", "list_skill_resources", "read_skill_resource", "search_skill_resources"} {
		if !names[want] {
			t.Fatalf("missing tool %s in %v", want, names)
		}
	}
	meta, err := stack.Service.Resolve(context.Background(), skillcontract.ResolveRequest{
		CatalogRequest: stack.CatalogRequest,
		Name:           "office-ppt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "office-ppt" {
		t.Fatalf("meta=%+v", meta)
	}
}
