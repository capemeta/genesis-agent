package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	multiagentmodel "genesis-agent/internal/runtime/multiagent/model"
	multiprojection "genesis-agent/internal/runtime/multiagent/projection"
)

func TestSubAgentProjectionHandlerListsFilteredEvents(t *testing.T) {
	sink := multiprojection.NewMemorySink(multiagentmodel.ProjectionChannelEnterprise)
	if err := sink.EmitProjection(context.Background(), multiagentmodel.ProjectionEvent{TenantID: "tenant-a", SessionID: "wanted", AgentID: "agent"}); err != nil {
		t.Fatal(err)
	}
	if err := sink.EmitProjection(context.Background(), multiagentmodel.ProjectionEvent{TenantID: "tenant-b", SessionID: "other", AgentID: "agent"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/subagents/events?session_id=wanted", nil)
	recorder := httptest.NewRecorder()
	NewSubAgentProjectionHandlerForTenant(sink, "tenant-a").List(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Body.String(); !strings.Contains(got, "wanted") || strings.Contains(got, "other") {
		t.Fatalf("body = %s", got)
	}
}

func TestSubAgentProjectionHandlerRejectsCallerTenant(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/subagents/events?tenant_id=other", nil)
	recorder := httptest.NewRecorder()
	NewSubAgentProjectionHandlerForTenant(multiprojection.NewMemorySink(multiagentmodel.ProjectionChannelEnterprise), "tenant-a").List(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
