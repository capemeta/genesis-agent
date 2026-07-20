package service

import (
	"testing"
)

func TestSubAgentDelegationContract(t *testing.T) {
	req := SubAgentDelegationRequest{
		TaskID:    "task-123",
		Goal:      "Generate PPTX Report",
		SkillName: "office-ppt",
	}

	if err := req.ValidateRequest(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	invalidReq := SubAgentDelegationRequest{}
	if err := invalidReq.ValidateRequest(); err == nil {
		t.Fatalf("expected validation error for empty request, got nil")
	}
}
