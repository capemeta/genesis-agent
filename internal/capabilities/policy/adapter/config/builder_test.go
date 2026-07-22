package config_test

import (
	"context"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	policyconfig "genesis-agent/internal/capabilities/policy/adapter/config"
	platformconfig "genesis-agent/internal/platform/config"
)

func TestBuildEvaluatorFullAccessAllowsSkillPaths(t *testing.T) {
	eval := policyconfig.BuildEvaluator(platformconfig.PolicyConfig{
		PermissionMode: "full_access",
		Defaults: platformconfig.PolicyDefaultsConfig{
			Unknown:            "ask",
			Dangerous:          "ask",
			Critical:           "deny",
			AllowedGrantScopes: []string{"once", "session"},
		},
	})
	cases := []approvalmodel.Request{
		{ToolName: "Skill", Action: approvalmodel.ActionSkillLoad},
		{ToolName: "Task", Action: approvalmodel.ActionSubAgentDelegate, Resource: approvalmodel.Resource{URI: "skill-fork:office-ppt"}},
		{ToolName: "run_skill_command", Action: approvalmodel.ActionCommandExec, Metadata: map[string]string{"skill_script": "true"}},
	}
	for _, req := range cases {
		res, err := eval.Evaluate(context.Background(), req)
		if err != nil {
			t.Fatalf("%s: %v", req.Action, err)
		}
		if res.Type != approvalmodel.PolicyAllow {
			t.Fatalf("%s (%s): got %+v, want Allow（full_access 下技能路径不应再 Ask）", req.ToolName, req.Action, res)
		}
	}
}

func TestBuildEvaluatorWorkspaceAutoAllowsSkillPathsDeniesInstall(t *testing.T) {
	eval := policyconfig.BuildEvaluator(platformconfig.PolicyConfig{
		PermissionMode: "workspace_auto",
		Defaults: platformconfig.PolicyDefaultsConfig{
			Unknown:            "ask",
			Dangerous:          "ask",
			Critical:           "deny",
			AllowedGrantScopes: []string{"once", "session"},
		},
	})
	allow := []approvalmodel.Request{
		{ToolName: "Task", Action: approvalmodel.ActionSubAgentDelegate},
		{ToolName: "run_skill_command", Action: approvalmodel.ActionCommandExec},
		{ToolName: "http_request", Action: approvalmodel.ActionHTTPRequest},
	}
	for _, req := range allow {
		res, err := eval.Evaluate(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if res.Type != approvalmodel.PolicyAllow {
			t.Fatalf("%s: got %+v, want Allow", req.Action, res)
		}
	}
	deny, err := eval.Evaluate(context.Background(), approvalmodel.Request{
		ToolName: "install_skill_from_source",
		Action:   approvalmodel.ActionSkillInstall,
	})
	if err != nil {
		t.Fatal(err)
	}
	if deny.Type != approvalmodel.PolicyDeny {
		t.Fatalf("skill install: got %+v, want Deny", deny)
	}
}
