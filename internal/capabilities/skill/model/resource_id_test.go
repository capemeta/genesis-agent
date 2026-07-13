package model_test

import (
	"testing"

	"genesis-agent/internal/capabilities/skill/model"
)

func TestQualifySkillResource(t *testing.T) {
	cases := []struct {
		pkg, name, resource, want string
	}{
		{"office-ppt", "office-ppt", "design.md", "office-ppt/design.md"},
		{"office-ppt", "office-ppt", "pptxgenjs.md", "office-ppt/pptxgenjs.md"},
		{"office-ppt", "", "design.md", "office-ppt/design.md"},
		{"", "office-ppt", "design.md", "office-ppt/design.md"},
		{"office-ppt", "office-ppt", "office-ppt/design.md", "office-ppt/design.md"},
		{"office-ppt", "office-ppt", "references/guide.md", "office-ppt/references/guide.md"},
		{"office-ppt", "office-ppt", "scripts/thumbnail.py", "office-ppt/scripts/thumbnail.py"},
		{"review", "review", "review/references/guide.md", "review/references/guide.md"},
		{"", "", "design.md", "design.md"},
	}
	for _, tc := range cases {
		got := string(model.QualifySkillResource(tc.pkg, tc.name, tc.resource))
		if got != tc.want {
			t.Fatalf("Qualify(%q,%q,%q)=%q want %q", tc.pkg, tc.name, tc.resource, got, tc.want)
		}
	}
}

func TestResourceLookupCandidatesIncludesShortAndQualified(t *testing.T) {
	cands := model.ResourceLookupCandidates("office-ppt", "office-ppt", "design.md")
	joined := stringsJoin(cands)
	if !contains(cands, "design.md") || !contains(cands, "office-ppt/design.md") {
		t.Fatalf("candidates=%v joined=%s", cands, joined)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func stringsJoin(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ","
		}
		out += item
	}
	return out
}
