package react

import (
	"errors"
	"strings"
	"testing"
)

func TestExtractToolFailureLogFieldsFromJSON(t *testing.T) {
	result := `{"ok":false,"failure_kind":"dependency_missing","stdout":"","stderr":"Cannot find module 'pptxgenjs'","error":"script exit_code=1"}`
	kind, stdout, stderr := extractToolFailureLogFields(result, errors.New("exit status 1"))
	if kind != "dependency_missing" {
		t.Fatalf("kind=%q", kind)
	}
	if !strings.Contains(stderr, "pptxgenjs") {
		t.Fatalf("stderr=%q", stderr)
	}
	_ = stdout
}

func TestExtractToolFailureLogFieldsInfersPathContract(t *testing.T) {
	kind, _, _ := extractToolFailureLogFields("", errors.New("invalid_input: EXECUTION_PATH_CONTRACT_VIOLATION: /"))
	if kind != "path_contract_violation" {
		t.Fatalf("kind=%q", kind)
	}
}
