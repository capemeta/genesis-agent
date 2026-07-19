package glob

import (
	"encoding/json"
	"testing"
)

func TestIsExactGlobPattern(t *testing.T) {
	for _, pattern := range []string{"ultra5-comparison-summary.md", "docs/report.md", `docs\\report.md`} {
		if !isExactGlobPattern(pattern) {
			t.Fatalf("%q should be exact", pattern)
		}
	}
	for _, pattern := range []string{"*.md", "**/*.md", "docs/[ab].md", "docs/?.md"} {
		if isExactGlobPattern(pattern) {
			t.Fatalf("%q should not be exact", pattern)
		}
	}
}

func TestGlobResultEmptyMatchesIsSuccessArray(t *testing.T) {
	raw, err := json.Marshal(globResult(".", "slide-*.jpeg", nil, false, ""))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		OK         bool     `json:"ok"`
		Matches    []string `json:"matches"`
		MatchCount int      `json:"match_count"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.MatchCount != 0 || out.Matches == nil || len(out.Matches) != 0 {
		t.Fatalf("empty glob result = %+v (raw=%s)", out, string(raw))
	}
}
