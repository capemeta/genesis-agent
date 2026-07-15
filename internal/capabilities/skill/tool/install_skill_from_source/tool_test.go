package install_skill_from_source

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	marketcontract "genesis-agent/internal/capabilities/package/marketplace/contract"
	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

type stubApproval struct {
	decision approvalmodel.Decision
}

func (s stubApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return s.decision, nil
}

type stubInstaller struct {
	result marketcontract.InstallFromSourceResult
	last   marketcontract.InstallFromSourceRequest
}

func (s *stubInstaller) InstallFromSource(_ context.Context, req marketcontract.InstallFromSourceRequest) marketcontract.InstallFromSourceResult {
	s.last = req
	return s.result
}

func TestToolApprovalDenied(t *testing.T) {
	tool, err := New(Deps{
		Installer: &stubInstaller{},
		Approval:  stubApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionDenied, Reason: "no"}},
		Product:   "cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(context.Background(), `{"source":"https://github.com/a/b/tree/main/skills/x"}`)
	if err != nil {
		t.Fatal(err)
	}
	var payload resultPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.OK || payload.FailureKind != "approval_denied" {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestToolDelegatesInstall(t *testing.T) {
	inst := &stubInstaller{result: marketcontract.InstallFromSourceResult{
		Skills:    []string{"demo"},
		Specs:     []string{"demo@github-a-b"},
		Effective: "next_turn",
		Message:   "ok",
	}}
	tool, err := New(Deps{
		Installer: inst,
		Approval:  stubApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionApproved}},
		Product:   "cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(context.Background(), `{"source":"github:a/b#skills/demo","skill_path":"skills/demo"}`)
	if err != nil {
		t.Fatal(err)
	}
	var payload resultPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Skills[0] != "demo" {
		t.Fatalf("payload=%+v", payload)
	}
	if !strings.Contains(payload.Message, "下一回合") {
		t.Fatalf("message=%s", payload.Message)
	}
	if inst.last.SourceInput != "github:a/b#skills/demo" || inst.last.SkillPath != "skills/demo" {
		t.Fatalf("last=%+v", inst.last)
	}
	_ = marketmodel.InstallScopeUser
}
