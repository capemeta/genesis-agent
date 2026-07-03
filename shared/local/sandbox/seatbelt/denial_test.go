package seatbelt

import (
	"strings"
	"testing"
)

func TestParseDenialsExtractsViolations(t *testing.T) {
	stderr := `some normal output
sandbox-exec: GENESIS_SANDBOX: file-read* deny /etc/passwd
sandbox-exec: GENESIS_SANDBOX: network-outbound deny
`
	violations := ParseDenials(stderr)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %#v", len(violations), violations)
	}
	if violations[0].Operation == "" {
		t.Fatalf("expected non-empty operation for first violation, got %#v", violations[0])
	}
}

func TestHasDenialReturnsTrueForMarker(t *testing.T) {
	stderr := "sandbox-exec: GENESIS_SANDBOX: deny"
	if !HasDenial(stderr) {
		t.Fatal("expected HasDenial=true for GENESIS_SANDBOX marker")
	}
}

func TestHasDenialReturnsFalseForNormalOutput(t *testing.T) {
	stderr := "some normal output without the marker"
	if HasDenial(stderr) {
		t.Fatal("expected HasDenial=false for normal stderr")
	}
}

func TestParseDenialsEmptyInput(t *testing.T) {
	violations := ParseDenials("")
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations for empty input, got %d", len(violations))
	}
}

func TestParseDenialsRawLinePreserved(t *testing.T) {
	line := "sandbox-exec: GENESIS_SANDBOX: file-write* deny /work/.git"
	violations := ParseDenials(line)
	if len(violations) == 0 {
		t.Fatal("expected at least 1 violation")
	}
	if !strings.Contains(violations[0].Raw, "GENESIS_SANDBOX") {
		t.Fatalf("Raw should contain marker, got %q", violations[0].Raw)
	}
}
