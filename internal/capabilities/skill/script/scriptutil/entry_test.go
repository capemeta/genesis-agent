package scriptutil_test

import (
	"testing"

	"genesis-agent/internal/capabilities/skill/script/scriptutil"
)

func TestIsExecutableScriptEntry(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"office-ppt/scripts/inspect_pptx.py", true},
		{"office-ppt/scripts/render_pptx_preview.py", true},
		{"office-ppt/scripts/thumbnail.py", true},
		{"office-ppt/scripts/office/unpack.py", true},
		{"office-ppt/scripts/office/pack.py", true},
		{"office-ppt/scripts/path_contract.py", false},
		{"office-ppt/scripts/office/helpers/merge_runs.py", false},
		{"office-ppt/scripts/office/validators/pptx.py", false},
		{"path_contract.py", false},
		{"scripts/__init__.py", false},
		{"scripts/_helper.py", false},
	}
	for _, tc := range cases {
		if got := scriptutil.IsExecutableScriptEntry(tc.in); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.in, got, tc.want)
		}
	}
}
