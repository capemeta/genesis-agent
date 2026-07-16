package projection

import (
	"context"
	"testing"

	"genesis-agent/internal/runtime/multiagent/model"
)

func TestMemorySinkFiltersAndCopiesProjectionEvents(t *testing.T) {
	sink := NewMemorySink(model.ProjectionChannelEnterprise)
	if err := sink.EmitProjection(context.Background(), model.ProjectionEvent{SessionID: "session-a", AgentID: "agent-a", Metadata: map[string]string{"state": "done"}}); err != nil {
		t.Fatal(err)
	}
	if err := sink.EmitProjection(context.Background(), model.ProjectionEvent{SessionID: "session-b", AgentID: "agent-b"}); err != nil {
		t.Fatal(err)
	}
	events, err := sink.ListProjectionEvents(context.Background(), model.ProjectionQuery{SessionID: "session-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Channel != model.ProjectionChannelEnterprise {
		t.Fatalf("events = %#v", events)
	}
	events[0].Metadata["state"] = "changed"
	if sink.Events()[0].Metadata["state"] != "done" {
		t.Fatal("projection metadata must be copied")
	}
}
