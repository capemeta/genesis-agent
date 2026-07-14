package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLandLocalProducedToRunOutputCopiesDeliveryArtifacts(t *testing.T) {
	skillDir := t.TempDir()
	outputRoot := t.TempDir()
	src := filepath.Join(skillDir, "deck.pptx")
	// Minimal ZIP/OOXML-ish bytes so gate can classify; empty may fail gate but path landing still matters.
	if err := os.WriteFile(src, []byte("PK\x03\x04fake-pptx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "note.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	arts, warnings := landLocalProducedToRunOutput(skillDir, outputRoot, []string{"deck.pptx", "note.txt"})
	if len(warnings) != 0 {
		// gate may warn on fake pptx; landing must still succeed
		t.Logf("warnings=%v", warnings)
	}
	if len(arts) != 2 {
		t.Fatalf("arts=%+v", arts)
	}
	for _, art := range arts {
		if filepath.Clean(filepath.Dir(art.Path)) != filepath.Clean(outputRoot) {
			t.Fatalf("artifact not under output root: %+v", art)
		}
		if _, err := os.Stat(art.Path); err != nil {
			t.Fatalf("missing landed artifact: %v", err)
		}
	}
	// 源文件仍保留在 skill cwd
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source should remain: %v", err)
	}
}
