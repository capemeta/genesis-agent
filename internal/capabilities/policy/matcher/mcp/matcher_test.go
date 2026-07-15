package mcp_test

import (
	"context"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	policymcp "genesis-agent/internal/capabilities/policy/matcher/mcp"
	"genesis-agent/internal/platform/config"
)

func TestMatcherHitsMCPCall(t *testing.T) {
	t.Parallel()
	m := policymcp.New(config.PolicyDefaultsConfig{Dangerous: "ask"})
	res, ok, err := m.Match(context.Background(), approvalmodel.Request{
		Action:   approvalmodel.ActionMCPCall,
		ToolName: "mcp__demo__list",
		Resource: approvalmodel.Resource{URI: "mcp://demo/list"},
	})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if res.Type != approvalmodel.PolicyAsk {
		t.Fatalf("type=%s", res.Type)
	}
}

func TestMatcherDenyPrefix(t *testing.T) {
	t.Parallel()
	m := policymcp.New(config.PolicyDefaultsConfig{})
	m.DenyPrefixes = []string{"mcp__danger__*"}
	res, ok, err := m.Match(context.Background(), approvalmodel.Request{
		Action:   approvalmodel.ActionMCPCall,
		ToolName: "mcp__danger__rm",
	})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if res.Type != approvalmodel.PolicyDeny {
		t.Fatalf("type=%s", res.Type)
	}
}

func TestMatcherIgnoresNonMCP(t *testing.T) {
	t.Parallel()
	m := policymcp.New(config.PolicyDefaultsConfig{})
	_, ok, err := m.Match(context.Background(), approvalmodel.Request{
		Action:   approvalmodel.ActionFileRead,
		ToolName: "read_file",
	})
	if err != nil || ok {
		t.Fatalf("expected no match, ok=%v err=%v", ok, err)
	}
}
