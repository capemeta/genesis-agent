package read_file

import (
	"errors"
	"testing"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
)

func TestLooksLikeImagePath(t *testing.T) {
	if !looksLikeImagePath("slide-1.jpg") || !looksLikeImagePath(`C:\tmp\a.PNG`) {
		t.Fatal("expected image paths")
	}
	if looksLikeImagePath("notes.md") {
		t.Fatal("md should not look like image")
	}
}

func TestMissingImageGuidance(t *testing.T) {
	err := fscontract.NewError(fscontract.ErrCodeNotFound, "slide-1.jpg", errors.New("missing"))
	got, ok := missingImageGuidance("slide-1.jpg", err)
	if !ok || got.SuggestedAction != "use_run_skill_command_for_skill_cwd" || got.Error != string(fscontract.ErrCodeNotFound) {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	if _, ok := missingImageGuidance("a.md", err); ok {
		t.Fatal("non-image should not get image guidance")
	}
}
