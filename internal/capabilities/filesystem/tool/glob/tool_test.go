package glob

import "testing"

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
