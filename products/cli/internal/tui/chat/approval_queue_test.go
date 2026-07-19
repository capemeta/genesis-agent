package chat

import (
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	approval "genesis-agent/products/cli/internal/approval"
)

func TestApprovalQueueDoesNotOrphan(t *testing.T) {
	t.Parallel()
	m := Model{}
	ch1 := make(chan approvalmodel.Decision, 1)
	ch2 := make(chan approvalmodel.Decision, 1)
	m.enqueueApproval(approval.ApprovalRequiredMsg{
		Request:  approvalmodel.Request{ToolName: "read_file", Resource: approvalmodel.Resource{Display: "a.jpg"}},
		ResultCh: ch1,
	})
	m.enqueueApproval(approval.ApprovalRequiredMsg{
		Request:  approvalmodel.Request{ToolName: "read_file", Resource: approvalmodel.Resource{Display: "b.jpg"}},
		ResultCh: ch2,
	})
	if m.activeApproval == nil || m.activeApproval.Request.Resource.Display != "a.jpg" {
		t.Fatalf("active=%+v", m.activeApproval)
	}
	if len(m.pendingApprovals) != 1 {
		t.Fatalf("pending=%d", len(m.pendingApprovals))
	}

	m.resolveActiveApproval(approvalmodel.Decision{Type: approvalmodel.DecisionApprovedForScope, Scope: approvalmodel.GrantScopeSession})
	select {
	case dec := <-ch1:
		if dec.Type != approvalmodel.DecisionApprovedForScope {
			t.Fatalf("ch1=%+v", dec)
		}
	default:
		t.Fatal("ch1 not signaled")
	}
	if m.activeApproval == nil || m.activeApproval.Request.Resource.Display != "b.jpg" {
		t.Fatalf("expected promote b.jpg, got %+v", m.activeApproval)
	}
	if len(m.pendingApprovals) != 0 {
		t.Fatalf("pending should be empty")
	}

	m.resolveActiveApproval(approvalmodel.Decision{Type: approvalmodel.DecisionApproved, Scope: approvalmodel.GrantScopeOnce})
	select {
	case <-ch2:
	default:
		t.Fatal("ch2 not signaled")
	}
	if m.activeApproval != nil || m.approvalFocus {
		t.Fatal("should clear after last approval")
	}
}

func TestRejectAllPendingApprovals(t *testing.T) {
	t.Parallel()
	m := Model{}
	ch1 := make(chan approvalmodel.Decision, 1)
	ch2 := make(chan approvalmodel.Decision, 1)
	m.enqueueApproval(approval.ApprovalRequiredMsg{ResultCh: ch1})
	m.enqueueApproval(approval.ApprovalRequiredMsg{ResultCh: ch2})
	m.rejectAllPendingApprovals(approvalmodel.Decision{Type: approvalmodel.DecisionAbort})
	if m.activeApproval != nil || len(m.pendingApprovals) != 0 {
		t.Fatal("should clear all")
	}
	select {
	case dec := <-ch1:
		if dec.Type != approvalmodel.DecisionAbort {
			t.Fatalf("ch1=%+v", dec)
		}
	default:
		t.Fatal("ch1 missing")
	}
	select {
	case dec := <-ch2:
		if dec.Type != approvalmodel.DecisionAbort {
			t.Fatalf("ch2=%+v", dec)
		}
	default:
		t.Fatal("ch2 missing")
	}
}
