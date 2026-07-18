package model

import (
	"path/filepath"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func TestExpandLogicalPath(t *testing.T) {
	ws := execmodel.ExecutionWorkspace{
		WorkDir:   filepath.FromSlash("/workspace/work"),
		InputDir:  filepath.FromSlash("/workspace/input"),
		OutputDir: filepath.FromSlash("/workspace/output"),
		TmpDir:    filepath.FromSlash("/workspace/tmp"),
		SkillDir:  filepath.FromSlash("/workspace/skills"),
	}

	tests := []struct {
		raw      string
		expected string
	}{
		{"src/main.go", filepath.Join(ws.WorkDir, "src", "main.go")},
		{"./src/main.go", filepath.Join(ws.WorkDir, "src", "main.go")},
		{"README.md", filepath.Join(ws.WorkDir, "README.md")},
		{"input/data.csv", filepath.Join(ws.InputDir, "data.csv")},
		{"./input/data.csv", filepath.Join(ws.InputDir, "data.csv")},
		{"$INPUT_DIR/data.csv", filepath.Join(ws.InputDir, "data.csv")},
		{"output/report.pdf", filepath.Join(ws.OutputDir, "report.pdf")},
		{"./output/report.pdf", filepath.Join(ws.OutputDir, "report.pdf")},
		{"$OUTPUT_DIR/report.pdf", filepath.Join(ws.OutputDir, "report.pdf")},
		{"skills/office-ppt/run.py", filepath.Join(ws.SkillDir, "office-ppt", "run.py")},
		{"$SKILL_DIR/office-ppt/run.py", filepath.Join(ws.SkillDir, "office-ppt", "run.py")},
		{"tmp/cache.json", filepath.Join(ws.TmpDir, "cache.json")},
		{"$TMPDIR/cache.json", filepath.Join(ws.TmpDir, "cache.json")},
		{"$WORK_DIR/foo.txt", filepath.Join(ws.WorkDir, "foo.txt")},
	}

	for _, tt := range tests {
		got, ok, err := ExpandLogicalPath(tt.raw, ws)
		if err != nil {
			t.Errorf("ExpandLogicalPath(%q) returned unexpected error: %v", tt.raw, err)
			continue
		}
		if !ok {
			t.Errorf("ExpandLogicalPath(%q) returned ok=false, expected true", tt.raw)
			continue
		}
		if got != tt.expected {
			t.Errorf("ExpandLogicalPath(%q) = %q, want %q", tt.raw, got, tt.expected)
		}
	}
}

func TestExpandLogicalPathInvalid(t *testing.T) {
	ws := execmodel.ExecutionWorkspace{
		WorkDir: filepath.FromSlash("/workspace/work"),
	}

	invalidPaths := []string{
		"../secret",
		"a/../secret",
		"input/../secret",
	}

	for _, path := range invalidPaths {
		_, _, err := ExpandLogicalPath(path, ws)
		if err == nil {
			t.Errorf("ExpandLogicalPath(%q) expected error, got nil", path)
		}
	}
}
