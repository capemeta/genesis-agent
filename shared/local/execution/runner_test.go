package execution

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func echoCommand() string {
	return "echo hello"
}

func exitCommand() string {
	if runtime.GOOS == "windows" {
		return "exit /b 7"
	}
	return "exit 7"
}

func longOutputCommand() string {
	if runtime.GOOS == "windows" {
		return "echo 1234567890"
	}
	return "printf 1234567890"
}

func TestMergeEnvOverridesDuplicateKeys(t *testing.T) {
	base := []string{"PATH=/usr/bin", "FOO=1", "Bar=keep"}
	got := mergeEnv(base, map[string]string{"PATH": "/venv/bin:/usr/bin", "FOO": "2"})
	pathVal, fooVal, barVal := "", "", ""
	pathCount, fooCount := 0, 0
	for _, item := range got {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		switch envKeyCanon(key) {
		case envKeyCanon("PATH"):
			pathCount++
			pathVal = value
		case envKeyCanon("FOO"):
			fooCount++
			fooVal = value
		case envKeyCanon("Bar"):
			barVal = value
		}
	}
	if pathCount != 1 || pathVal != "/venv/bin:/usr/bin" {
		t.Fatalf("PATH override failed: count=%d val=%q env=%v", pathCount, pathVal, got)
	}
	if fooCount != 1 || fooVal != "2" {
		t.Fatalf("FOO override failed: count=%d val=%q env=%v", fooCount, fooVal, got)
	}
	if barVal != "keep" {
		t.Fatalf("Bar should remain: %q env=%v", barVal, got)
	}
}

func TestDecodeCommandOutputPrefersUTF8AndFallsBackGBK(t *testing.T) {
	if got := decodeCommandOutput([]byte("hello")); got != "hello" {
		t.Fatalf("utf8=%q", got)
	}
	// 「价格」的 GBK 字节
	gbkPrice := []byte{0xbc, 0xdb, 0xb8, 0xf1}
	got := decodeCommandOutput(gbkPrice)
	if !strings.Contains(got, "价格") {
		t.Fatalf("gbk decode got %q (%v)", got, []byte(got))
	}
}

func TestRunnerRejectsPowerShellWithoutDedicatedRunner(t *testing.T) {
	runner := NewRunner()
	_, err := runner.Run(context.Background(), execmodel.Command{Command: echoCommand(), Shell: execmodel.ShellPowerShell}, execcontract.RunOptions{Timeout: 30 * time.Second})
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err) = %s, want %s", code, execcontract.ErrCodeInvalidInput)
	}
}
func TestRunnerRunEcho(t *testing.T) {
	runner := NewRunner()
	result, err := runner.Run(context.Background(), execmodel.Command{Command: echoCommand()}, execcontract.RunOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.TimedOut {
		t.Fatalf("TimedOut = true, stdout=%q stderr=%q error=%q", result.Stdout, result.Stderr, result.Error)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, stdout=%q stderr=%q error=%q", result.ExitCode, result.Stdout, result.Stderr, result.Error)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Fatalf("Stdout = %q, want contains hello", result.Stdout)
	}
}

func TestRunnerRunExitErrorReturnsResult(t *testing.T) {
	runner := NewRunner()
	result, err := runner.Run(context.Background(), execmodel.Command{Command: exitCommand()}, execcontract.RunOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.TimedOut {
		t.Fatalf("TimedOut = true, stdout=%q stderr=%q error=%q", result.Stdout, result.Stderr, result.Error)
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, stdout=%q stderr=%q error=%q, want 7", result.ExitCode, result.Stdout, result.Stderr, result.Error)
	}
}

func TestRunnerRunTruncatesOutput(t *testing.T) {
	runner := NewRunner()
	result, err := runner.Run(context.Background(), execmodel.Command{Command: longOutputCommand()}, execcontract.RunOptions{Timeout: 30 * time.Second, MaxOutputBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if result.TimedOut {
		t.Fatalf("TimedOut = true, stdout=%q stderr=%q error=%q", result.Stdout, result.Stderr, result.Error)
	}
	if !result.OutputTruncated {
		t.Fatalf("OutputTruncated = false, stdout=%q stderr=%q error=%q exit=%d", result.Stdout, result.Stderr, result.Error, result.ExitCode)
	}
	if len(result.Stdout) > 4 {
		t.Fatalf("len(stdout) = %d, want <= 4", len(result.Stdout))
	}
}
