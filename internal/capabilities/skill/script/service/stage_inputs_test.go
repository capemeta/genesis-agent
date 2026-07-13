package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func TestStageInputsResolvesFromOutputDir(t *testing.T) {
	root := t.TempDir()
	ws := execmodel.ExecutionWorkspace{
		InputDir:  filepath.Join(root, ".genesis", "runs", "r1", "input"),
		OutputDir: filepath.Join(root, ".genesis", "runs", "r1", "output"),
		WorkDir:   filepath.Join(root, ".genesis", "runs", "r1", "work"),
		TmpDir:    filepath.Join(root, ".genesis", "runs", "r1", "tmp"),
	}
	for _, d := range []string{ws.InputDir, ws.OutputDir, ws.WorkDir, ws.TmpDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	skillDir := filepath.Join(ws.WorkDir, "skills", "office-ppt")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(ws.OutputDir, "deck.pptx")
	if err := os.WriteFile(src, []byte("PK-fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	staged, err := stageInputs(root, ws, skillDir, []string{"deck.pptx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || staged[0] != "deck.pptx" {
		t.Fatalf("staged=%v", staged)
	}
	dest := filepath.Join(skillDir, "deck.pptx")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "PK-fake" {
		t.Fatalf("dest content=%q", data)
	}
}

func TestStageInputsResolvesLogicalOutputPrefix(t *testing.T) {
	root := t.TempDir()
	ws := execmodel.ExecutionWorkspace{InputDir: filepath.Join(root, "input"), OutputDir: filepath.Join(root, "output"), WorkDir: filepath.Join(root, "work"), TmpDir: filepath.Join(root, "tmp")}
	for _, d := range []string{ws.InputDir, ws.OutputDir, ws.WorkDir, ws.TmpDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	skillDir := filepath.Join(ws.WorkDir, "skills", "office-ppt")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.OutputDir, "out.pptx"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged, err := stageInputs(root, ws, skillDir, []string{"$OUTPUT_DIR/out.pptx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || staged[0] != "out.pptx" {
		t.Fatalf("staged=%v", staged)
	}
}

func TestStageInputsResolvesLogicalWorkPrefix(t *testing.T) {
	root := t.TempDir()
	ws := execmodel.ExecutionWorkspace{InputDir: filepath.Join(root, "input"), OutputDir: filepath.Join(root, "output"), WorkDir: filepath.Join(root, "work"), TmpDir: filepath.Join(root, "tmp")}
	for _, d := range []string{ws.InputDir, ws.OutputDir, ws.WorkDir, ws.TmpDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	skillDir := filepath.Join(ws.WorkDir, "skills", "office-ppt")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.WorkDir, "deck_gen.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged, err := stageInputs(root, ws, skillDir, []string{"$WORK_DIR/deck_gen.js"})
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || staged[0] != "deck_gen.js" {
		t.Fatalf("staged=%v", staged)
	}
}

func TestStageInputsStillRejectsOutsideWorkspace(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := execmodel.ExecutionWorkspace{InputDir: filepath.Join(root, "input"), OutputDir: filepath.Join(root, "output"), WorkDir: filepath.Join(root, "work"), TmpDir: filepath.Join(root, "tmp")}
	for _, d := range []string{ws.InputDir, ws.OutputDir, ws.WorkDir, ws.TmpDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	skillDir := filepath.Join(ws.WorkDir, "skills", "office-ppt")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside.pptx")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := stageInputs(root, ws, skillDir, []string{"../outside.pptx"})
	if err == nil || !strings.Contains(err.Error(), "工作区内") {
		t.Fatalf("err=%v", err)
	}
}
