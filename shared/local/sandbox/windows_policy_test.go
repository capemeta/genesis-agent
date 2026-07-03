package sandbox

import (
	"testing"

	win "genesis-agent/shared/local/sandbox/windows"
)

func TestWindowsProcessConstrainedSupportRejectsFilesystemAndNetwork(t *testing.T) {
	res := win.EvaluateProcessConstrainedSupport(win.FileSystemPolicy{RequiresFilesystem: true}, win.NetworkDisabled, win.ProcessPolicy{})
	if res.Supported || len(res.Reasons) < 2 {
		t.Fatalf("expected unsupported with reasons, got %#v", res)
	}
}

func TestWindowsProcessConstrainedPlanPreservesArgv(t *testing.T) {
	argv, err := win.BuildProcessConstrainedPlan([]string{"cmd.exe", "/d", "/c", "echo hi"})
	if err != nil {
		t.Fatalf("BuildProcessConstrainedPlan() error = %v", err)
	}
	if len(argv) != 4 || argv[3] != "echo hi" {
		t.Fatalf("argv changed: %#v", argv)
	}
}

func TestWindowsAppContainerFailsClosedUntilProfileSetupExists(t *testing.T) {
	res := win.EvaluateAppContainerSupport()
	if res.Supported || len(res.Reasons) == 0 {
		t.Fatalf("AppContainer support = %#v, want fail-closed reason", res)
	}
}
