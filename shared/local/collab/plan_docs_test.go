package collab

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFilePlanDocumentsRoundTrip(t *testing.T) {
	root := t.TempDir()
	docs := NewFilePlanDocuments(root)
	ctx := context.Background()
	rel, err := docs.Write(ctx, "sess-a", "# plan\n")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, filepath.FromSlash(rel))
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("missing file %s: %v", want, err)
	}
	gotRel, body, err := docs.Read(ctx, "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	if gotRel != rel || body != "# plan\n" {
		t.Fatalf("got %q %q", gotRel, body)
	}
}
