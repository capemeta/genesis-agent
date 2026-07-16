package clipboard

import (
	"errors"
	"strings"
	"testing"
)

func TestWriteWithBackendsLocalPrefersNative(t *testing.T) {
	calls := make([]string, 0, 3)
	err := writeWithBackends(
		"hello",
		copyEnvironment{},
		recordingBackend(&calls, "native", nil),
		recordingBackend(&calls, "powershell", nil),
		recordingBackend(&calls, "terminal", nil),
	)
	if err != nil {
		t.Fatalf("writeWithBackends() error = %v", err)
	}
	if got, want := strings.Join(calls, ","), "native"; got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func TestWriteWithBackendsWSLFallbackOrder(t *testing.T) {
	calls := make([]string, 0, 3)
	err := writeWithBackends(
		"hello",
		copyEnvironment{wsl: true},
		recordingBackend(&calls, "native", errors.New("native failed")),
		recordingBackend(&calls, "powershell", nil),
		recordingBackend(&calls, "terminal", nil),
	)
	if err != nil {
		t.Fatalf("writeWithBackends() error = %v", err)
	}
	if got, want := strings.Join(calls, ","), "native,powershell"; got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func TestWriteWithBackendsLocalFallsBackToTerminal(t *testing.T) {
	calls := make([]string, 0, 2)
	err := writeWithBackends(
		"hello",
		copyEnvironment{},
		recordingBackend(&calls, "native", errors.New("native failed")),
		recordingBackend(&calls, "powershell", nil),
		recordingBackend(&calls, "terminal", nil),
	)
	if err != nil {
		t.Fatalf("writeWithBackends() error = %v", err)
	}
	if got, want := strings.Join(calls, ","), "native,terminal"; got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func TestWriteWithBackendsSSHCopiesThroughTerminal(t *testing.T) {
	calls := make([]string, 0, 3)
	err := writeWithBackends(
		"hello",
		copyEnvironment{ssh: true},
		recordingBackend(&calls, "native", nil),
		recordingBackend(&calls, "powershell", nil),
		recordingBackend(&calls, "terminal", nil),
	)
	if err != nil {
		t.Fatalf("writeWithBackends() error = %v", err)
	}
	if got, want := strings.Join(calls, ","), "terminal"; got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func TestWriteWithBackendsReportsAllFailures(t *testing.T) {
	err := writeWithBackends(
		"hello",
		copyEnvironment{wsl: true},
		func(string) error { return errors.New("native failed") },
		func(string) error { return errors.New("powershell failed") },
		func(string) error { return errors.New("terminal failed") },
	)
	if err == nil {
		t.Fatal("writeWithBackends() error = nil, want aggregated error")
	}
	for _, part := range []string{"native failed", "powershell failed", "terminal failed"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error %q does not contain %q", err, part)
		}
	}
}

func TestOSC52Sequence(t *testing.T) {
	sequence, err := osc52Sequence("你好", false)
	if err != nil {
		t.Fatalf("osc52Sequence() error = %v", err)
	}
	if got, want := sequence, "\x1b]52;c;5L2g5aW9\x07"; got != want {
		t.Fatalf("sequence = %q, want %q", got, want)
	}

	tmuxSequence, err := osc52Sequence("hello", true)
	if err != nil {
		t.Fatalf("tmux osc52Sequence() error = %v", err)
	}
	if !strings.HasPrefix(tmuxSequence, "\x1bPtmux;\x1b\x1b]52;c;") || !strings.HasSuffix(tmuxSequence, "\x07\x1b\\") {
		t.Fatalf("unexpected tmux sequence: %q", tmuxSequence)
	}
}

func TestOSC52SequenceRejectsOversizedPayload(t *testing.T) {
	_, err := osc52Sequence(strings.Repeat("x", osc52MaxRawBytes+1), false)
	if err == nil {
		t.Fatal("osc52Sequence() error = nil, want size error")
	}
}

func recordingBackend(calls *[]string, name string, err error) copyBackend {
	return func(text string) error {
		*calls = append(*calls, name)
		if text != "hello" {
			return errors.New("unexpected text")
		}
		return err
	}
}
