package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	skillmodel "genesis-agent/internal/capabilities/skill/model"
	"genesis-agent/internal/capabilities/skill/packaging"
)

func TestPackageStorePersistsImmutableContentAddressedSnapshot(t *testing.T) {
	root := t.TempDir()
	authority := skillmodel.Authority{Kind: skillmodel.SourceKindHost, ID: "test"}
	files := []skillmodel.SkillPackageFile{
		{Resource: "demo/SKILL.md", Content: []byte("---\nname: demo\ndescription: Demo\n---\nbody")},
		{Resource: "demo/scripts/run.py", Content: []byte("print('ok')")},
	}
	raw := make([]packaging.File, 0, len(files))
	for _, file := range files {
		raw = append(raw, packaging.File{Resource: file.Resource, Content: file.Content})
	}
	snapshot, err := packaging.BuildSnapshot(authority, "demo", "v1", raw)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := NewPackageStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.SavePackageSnapshot(context.Background(), snapshot, files); err != nil {
		t.Fatal(err)
	}
	files[0].Content[0] = 'X'
	reader, err := NewPackageStore(root)
	if err != nil {
		t.Fatal(err)
	}
	gotSnapshot, gotFiles, err := reader.GetPackageSnapshot(context.Background(), snapshot.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if gotSnapshot.Digest != snapshot.Digest || len(gotFiles) != 2 || gotFiles[0].Content[0] == 'X' {
		t.Fatalf("snapshot=%+v files=%+v", gotSnapshot, gotFiles)
	}
	if err := reader.SavePackageSnapshot(context.Background(), snapshot, gotFiles); err != nil {
		t.Fatalf("idempotent save failed: %v", err)
	}
}

func TestPackageStoreRejectsTamperedPersistedContent(t *testing.T) {
	root := t.TempDir()
	authority := skillmodel.Authority{Kind: skillmodel.SourceKindHost, ID: "test"}
	files := []skillmodel.SkillPackageFile{{Resource: "demo/SKILL.md", Content: []byte("body")}}
	snapshot, err := packaging.BuildSnapshot(authority, "demo", "", []packaging.File{{Resource: files[0].Resource, Content: files[0].Content}})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewPackageStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePackageSnapshot(context.Background(), snapshot, files); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "runtime", "skill-packages", snapshot.Digest, "contents", "SKILL.md")
	if err := os.WriteFile(target, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GetPackageSnapshot(context.Background(), snapshot.Digest); err == nil {
		t.Fatal("tampered package content must be rejected")
	}
}
