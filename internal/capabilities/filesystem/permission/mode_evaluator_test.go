package permission

import (
	"context"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
)

func TestModeEvaluator_PlanMode(t *testing.T) {
	eval := NewModeEvaluator()
	ctx := context.Background()

	// Read action should be allowed
	res, err := eval.Evaluate(ctx, PermissionModePlan, approvalmodel.Request{
		Action: approvalmodel.ActionFileRead,
	})
	if err != nil || res.Type != approvalmodel.PolicyAllow {
		t.Fatalf("expected PolicyAllow for Plan read action, got %v, err: %v", res.Type, err)
	}

	// Write action should be PolicyDeny
	res, err = eval.Evaluate(ctx, PermissionModePlan, approvalmodel.Request{
		Action: approvalmodel.ActionFileWrite,
	})
	if err != nil || res.Type != approvalmodel.PolicyDeny {
		t.Fatalf("expected PolicyDeny for Plan write action, got %v, err: %v", res.Type, err)
	}

	// Command exec action should be PolicyDeny
	res, err = eval.Evaluate(ctx, PermissionModePlan, approvalmodel.Request{
		Action: approvalmodel.ActionCommandExec,
	})
	if err != nil || res.Type != approvalmodel.PolicyDeny {
		t.Fatalf("expected PolicyDeny for Plan command exec action, got %v, err: %v", res.Type, err)
	}
}

func TestModeEvaluator_ReadOnlyMode(t *testing.T) {
	eval := NewModeEvaluator()
	ctx := context.Background()

	// Read action should be allowed
	res, err := eval.Evaluate(ctx, PermissionModeReadOnly, approvalmodel.Request{
		Action: approvalmodel.ActionFileRead,
	})
	if err != nil || res.Type != approvalmodel.PolicyAllow {
		t.Fatalf("expected PolicyAllow for ReadOnly read action, got %v, err: %v", res.Type, err)
	}

	// Write action should be PolicyDeny (Hard Deny)
	res, err = eval.Evaluate(ctx, PermissionModeReadOnly, approvalmodel.Request{
		Action: approvalmodel.ActionFileWrite,
	})
	if err != nil || res.Type != approvalmodel.PolicyDeny {
		t.Fatalf("expected PolicyDeny for ReadOnly write action, got %v, err: %v", res.Type, err)
	}

	// HTTP Request should be PolicyDeny (Hard Deny)
	res, err = eval.Evaluate(ctx, PermissionModeReadOnly, approvalmodel.Request{
		Action: approvalmodel.ActionHTTPRequest,
	})
	if err != nil || res.Type != approvalmodel.PolicyDeny {
		t.Fatalf("expected PolicyDeny for ReadOnly HTTP request, got %v, err: %v", res.Type, err)
	}
}

func TestModeEvaluator_ProtectedWriteMode(t *testing.T) {
	eval := NewModeEvaluator()
	ctx := context.Background()

	// Read action should be allowed
	res, err := eval.Evaluate(ctx, PermissionModeProtectedWrite, approvalmodel.Request{
		Action: approvalmodel.ActionFileRead,
	})
	if err != nil || res.Type != approvalmodel.PolicyAllow {
		t.Fatalf("expected PolicyAllow for ProtectedWrite read action, got %v, err: %v", res.Type, err)
	}

	// Write action should trigger PolicyAsk with suggested scopes
	res, err = eval.Evaluate(ctx, PermissionModeProtectedWrite, approvalmodel.Request{
		Action: approvalmodel.ActionFileWrite,
	})
	if err != nil || res.Type != approvalmodel.PolicyAsk {
		t.Fatalf("expected PolicyAsk for ProtectedWrite write action, got %v, err: %v", res.Type, err)
	}
	if len(res.SuggestedScopes) == 0 {
		t.Fatalf("expected suggested scopes for ProtectedWrite write action")
	}

	// Command exec should trigger PolicyAsk
	res, err = eval.Evaluate(ctx, PermissionModeProtectedWrite, approvalmodel.Request{
		Action: approvalmodel.ActionCommandExec,
	})
	if err != nil || res.Type != approvalmodel.PolicyAsk {
		t.Fatalf("expected PolicyAsk for ProtectedWrite command exec action, got %v, err: %v", res.Type, err)
	}
}

func TestModeEvaluator_AgentMode(t *testing.T) {
	eval := NewModeEvaluator()
	ctx := context.Background()

	// Workspace write action should be allowed
	res, err := eval.Evaluate(ctx, PermissionModeAgent, approvalmodel.Request{
		Action: approvalmodel.ActionFileWrite,
		Resource: approvalmodel.Resource{
			Metadata: map[string]string{"scope": "workspace"},
		},
	})
	if err != nil || res.Type != approvalmodel.PolicyAllow {
		t.Fatalf("expected PolicyAllow for Agent workspace write, got %v, err: %v", res.Type, err)
	}

	// External write action should trigger PolicyAsk
	res, err = eval.Evaluate(ctx, PermissionModeAgent, approvalmodel.Request{
		Action: approvalmodel.ActionFileWrite,
		Resource: approvalmodel.Resource{
			Metadata: map[string]string{"scope": "external"},
		},
	})
	if err != nil || res.Type != approvalmodel.PolicyAsk {
		t.Fatalf("expected PolicyAsk for Agent external write, got %v, err: %v", res.Type, err)
	}
}

func TestModeEvaluator_FullAccessMode(t *testing.T) {
	eval := NewModeEvaluator()
	ctx := context.Background()

	// Read, Write, CommandExec, Skill/SubAgent 路径均应放行
	actions := []approvalmodel.Action{
		approvalmodel.ActionFileRead,
		approvalmodel.ActionFileWrite,
		approvalmodel.ActionCommandExec,
		approvalmodel.ActionHTTPRequest,
		approvalmodel.ActionSkillLoad,
		approvalmodel.ActionSkillResourceRead,
		approvalmodel.ActionSubAgentDelegate,
	}

	for _, act := range actions {
		res, err := eval.Evaluate(ctx, PermissionModeFullAccess, approvalmodel.Request{
			Action: act,
		})
		if err != nil || res.Type != approvalmodel.PolicyAllow {
			t.Fatalf("expected PolicyAllow for FullAccess action %s, got %v, err: %v", act, res.Type, err)
		}
	}

	// run_skill_command 形态：command.exec + tool 名
	res, err := eval.Evaluate(ctx, PermissionModeFullAccess, approvalmodel.Request{
		ToolName: "run_skill_command",
		Action:   approvalmodel.ActionCommandExec,
	})
	if err != nil || res.Type != approvalmodel.PolicyAllow {
		t.Fatalf("full_access run_skill_command: got %+v err=%v, want Allow", res, err)
	}
}

func TestModeEvaluator_ProtectedPathBehavior(t *testing.T) {
	eval := NewModeEvaluator()
	ctx := context.Background()

	// 1. FullAccess Mode:
	// SystemCritical write -> PolicyDeny
	res, err := eval.Evaluate(ctx, PermissionModeFullAccess, approvalmodel.Request{
		Action:   approvalmodel.ActionFileWrite,
		Metadata: map[string]string{"critical": "true"},
	})
	if err != nil || res.Type != approvalmodel.PolicyDeny {
		t.Fatalf("expected PolicyDeny for FullAccess SystemCritical write, got %v, err: %v", res.Type, err)
	}

	// Protected path write (e.g. AuthCredential/Hosts/Persistence) -> PolicyAsk
	res, err = eval.Evaluate(ctx, PermissionModeFullAccess, approvalmodel.Request{
		Action:   approvalmodel.ActionFileWrite,
		Metadata: map[string]string{"protected": "true"},
	})
	if err != nil || res.Type != approvalmodel.PolicyAsk {
		t.Fatalf("expected PolicyAsk for FullAccess protected path write, got %v, err: %v", res.Type, err)
	}

	// 2. Non-FullAccess Modes (Agent, ProtectedWrite, ReadOnly, Plan):
	// Any protected path write -> PolicyDeny (Hard Deny)
	nonFullModes := []PermissionMode{PermissionModeAgent, PermissionModeProtectedWrite, PermissionModeReadOnly, PermissionModePlan}
	for _, mode := range nonFullModes {
		res, err := eval.Evaluate(ctx, mode, approvalmodel.Request{
			Action:   approvalmodel.ActionFileWrite,
			Metadata: map[string]string{"protected": "true"},
		})
		if err != nil || res.Type != approvalmodel.PolicyDeny {
			t.Fatalf("expected PolicyDeny for mode %s on protected path write, got %v, err: %v", mode, res.Type, err)
		}
	}
}

func TestModeEvaluator_ProtectedReadBehavior(t *testing.T) {
	eval := NewModeEvaluator()
	ctx := context.Background()

	// Reading normal path in Agent mode -> PolicyAllow
	res, err := eval.Evaluate(ctx, PermissionModeAgent, approvalmodel.Request{
		Action: approvalmodel.ActionFileRead,
	})
	if err != nil || res.Type != approvalmodel.PolicyAllow {
		t.Fatalf("expected PolicyAllow for normal read in Agent mode, got %v", res.Type)
	}

	// Reading protected path in Agent / ProtectedWrite / ReadOnly -> PolicyAsk
	modes := []PermissionMode{PermissionModeAgent, PermissionModeProtectedWrite, PermissionModeReadOnly}
	for _, m := range modes {
		res, err = eval.Evaluate(ctx, m, approvalmodel.Request{
			Action:   approvalmodel.ActionFileRead,
			Metadata: map[string]string{"protected": "true"},
		})
		if err != nil || res.Type != approvalmodel.PolicyAsk {
			t.Fatalf("expected PolicyAsk for protected path read in %s mode, got %v", m, res.Type)
		}
	}

	// Reading protected path in Plan mode -> PolicyDeny
	res, err = eval.Evaluate(ctx, PermissionModePlan, approvalmodel.Request{
		Action:   approvalmodel.ActionFileRead,
		Metadata: map[string]string{"protected": "true"},
	})
	if err != nil || res.Type != approvalmodel.PolicyDeny {
		t.Fatalf("expected PolicyDeny for protected path read in Plan mode, got %v", res.Type)
	}
}

func TestModeEvaluator_WorkspaceAutoMode(t *testing.T) {
	eval := NewModeEvaluator()
	ctx := context.Background()

	allowActions := []approvalmodel.Action{
		approvalmodel.ActionFileRead,
		approvalmodel.ActionFileWrite,
		approvalmodel.ActionCommandExec,
		approvalmodel.ActionHTTPRequest,
		approvalmodel.ActionMCPCall,
		approvalmodel.ActionSkillLoad,
		approvalmodel.ActionSubAgentDelegate,
	}
	for _, act := range allowActions {
		res, err := eval.Evaluate(ctx, PermissionModeWorkspaceAuto, approvalmodel.Request{Action: act})
		if err != nil || res.Type != approvalmodel.PolicyAllow {
			t.Fatalf("workspace_auto action %s: got %+v err=%v, want Allow", act, res, err)
		}
	}

	res, err := eval.Evaluate(ctx, PermissionModeWorkspaceAuto, approvalmodel.Request{
		Action:   approvalmodel.ActionFileWrite,
		Metadata: map[string]string{"scope": "external"},
	})
	if err != nil || res.Type != approvalmodel.PolicyDeny {
		t.Fatalf("workspace_auto external write: got %+v err=%v, want Deny", res, err)
	}

	res, err = eval.Evaluate(ctx, PermissionModeWorkspaceAuto, approvalmodel.Request{
		Action: approvalmodel.ActionSkillInstall,
	})
	if err != nil || res.Type != approvalmodel.PolicyDeny {
		t.Fatalf("workspace_auto skill install: got %+v err=%v, want Deny", res, err)
	}

	res, err = eval.Evaluate(ctx, PermissionModeWorkspaceAuto, approvalmodel.Request{
		Action:   approvalmodel.ActionFileWrite,
		Metadata: map[string]string{"protected": "true"},
	})
	if err != nil || res.Type != approvalmodel.PolicyDeny {
		t.Fatalf("workspace_auto protected write: got %+v err=%v, want Deny", res, err)
	}
}

func TestNarrowPermissionMode(t *testing.T) {
	// Subagent cannot escalate beyond parent mode
	if got := NarrowPermissionMode(PermissionModeAgent, PermissionModeFullAccess); got != PermissionModeAgent {
		t.Fatalf("NarrowPermissionMode(agent, full_access) = %s, want agent", got)
	}
	if got := NarrowPermissionMode(PermissionModePlan, PermissionModeAgent); got != PermissionModePlan {
		t.Fatalf("NarrowPermissionMode(plan, agent) = %s, want plan", got)
	}
	if got := NarrowPermissionMode(PermissionModeWorkspaceAuto, PermissionModeFullAccess); got != PermissionModeWorkspaceAuto {
		t.Fatalf("NarrowPermissionMode(workspace_auto, full_access) = %s, want workspace_auto", got)
	}

	// Subagent can narrow permission
	if got := NarrowPermissionMode(PermissionModeAgent, PermissionModePlan); got != PermissionModePlan {
		t.Fatalf("NarrowPermissionMode(agent, plan) = %s, want plan", got)
	}
	if got := NarrowPermissionMode(PermissionModeFullAccess, PermissionModeProtectedWrite); got != PermissionModeProtectedWrite {
		t.Fatalf("NarrowPermissionMode(full_access, protected_write) = %s, want protected_write", got)
	}
	if got := NarrowPermissionMode(PermissionModeFullAccess, PermissionModeWorkspaceAuto); got != PermissionModeWorkspaceAuto {
		t.Fatalf("NarrowPermissionMode(full_access, workspace_auto) = %s, want workspace_auto", got)
	}
}


