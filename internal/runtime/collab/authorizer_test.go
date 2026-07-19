package collab

import (
	"context"
	"testing"

	"genesis-agent/internal/capabilities/tool/gateway"
	"genesis-agent/internal/platform/contextutil"
)

func TestAuthorizerFailClosedOnStoreError(t *testing.T) {
	ctx := WithStore(context.Background(), failStore{})
	ctx = contextutil.WithSessionID(ctx, "sess-1")
	dec, err := Authorizer{}.AuthorizeTool(ctx, gateway.AuthorizationRequest{ToolName: "read_file"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("expected deny when store fails")
	}
}

func TestAuthorizerAllowsWritePlanAfterEnterInStore(t *testing.T) {
	store := NewMemoryStore()
	ctx := WithStore(context.Background(), store)
	ctx = contextutil.WithSessionID(ctx, "sess-1")
	ctx = WithMode(ctx, ModeDefault) // context 仍为 default
	if err := store.Set(ctx, "sess-1", SessionState{Mode: ModePlan}); err != nil {
		t.Fatal(err)
	}
	dec, err := Authorizer{}.AuthorizeTool(ctx, gateway.AuthorizationRequest{ToolName: "write_implementation_plan"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatalf("expected allow: %s", dec.Reason)
	}
}
