package service

import (
	"context"
	"testing"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func TestSubAgentSandboxRunner(t *testing.T) {
	reconciler := NewWorkspacePatchReconciler()
	runner := NewSubAgentSandboxRunner(reconciler)

	cmd := execmodel.Command{Command: "pip install numpy"}
	profile := execmodel.SandboxProfile{Provider: "docker"}
	opts := execcontract.RunOptions{}

	res, err := runner.RunInSandbox(context.Background(), cmd, profile, opts)
	if err != nil {
		t.Fatalf("RunInSandbox 预期成功，实际报错: %v", err)
	}

	if res.Environment != execmodel.EnvironmentSandbox {
		t.Errorf("Environment 预期为 sandbox, 实际为 %v", res.Environment)
	}
}
