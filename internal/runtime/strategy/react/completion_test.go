package react

import (
	"context"
	"testing"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type incompleteWorkspaceGuard struct{}

func (incompleteWorkspaceGuard) InitializeRun(context.Context, workmodel.PreparedRun) error {
	return nil
}
func (incompleteWorkspaceGuard) EvaluateCompletion(context.Context, workmodel.PreparedRun) (workcontract.CompletionDecision, error) {
	return workcontract.CompletionDecision{Complete: false, Reminder: "missing project delta"}, nil
}
func (incompleteWorkspaceGuard) ReleaseRun(workmodel.PreparedRun) {}

func TestRunCompletionPendingIncludesWorkspaceGuard(t *testing.T) {
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: workmodel.RunManifest{RunID: "run"}})
	ctx = workcontract.WithCompletionGuard(ctx, incompleteWorkspaceGuard{})
	pending, reminder, err := runCompletionPending(ctx)
	if err != nil || !pending || reminder != "missing project delta" {
		t.Fatalf("pending=%t reminder=%q err=%v", pending, reminder, err)
	}
}
