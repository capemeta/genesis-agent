//go:build windows

package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func TestRunArgvProcessConstrainedSmoke(t *testing.T) {
	runner := NewRunner()
	result, err := runner.RunArgvProcessConstrained(context.Background(), ArgvCommand{
		Argv:           []string{windowsShell(), "/d", "/c", "echo sandbox"},
		DisplayCommand: "echo sandbox",
		Shell:          execmodel.ShellCmd,
	}, execcontract.RunOptions{Timeout: 5 * time.Second})
	if err != nil {
		if strings.Contains(err.Error(), "CreateRestrictedToken failed") {
			t.Skipf("host cannot create restricted token: %v", err)
		}
		t.Fatalf("RunArgvProcessConstrained() error = %v", err)
	}
	if result.ExitCode != 0 || !strings.Contains(result.Stdout, "sandbox") {
		t.Fatalf("result = %+v", result)
	}
}
