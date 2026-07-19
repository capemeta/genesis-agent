package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestInputSnapshotStoreLookupCAS(t *testing.T) {
	root := t.TempDir()
	store, err := NewInputSnapshotStore(root)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("hello-cas")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	put, err := store.PutCAS(context.Background(), "run-1", "a.md", bytes.NewReader(content), 1024)
	if err != nil || put.Reused || put.SHA256 != digest {
		t.Fatalf("put=%+v err=%v", put, err)
	}
	hit, ok, err := store.LookupCAS(context.Background(), "run-1", digest, "a.md")
	if err != nil || !ok || !hit.Reused || hit.Path != put.Path || hit.Size != int64(len(content)) {
		t.Fatalf("lookup hit=%+v ok=%v err=%v", hit, ok, err)
	}
	_, ok, err = store.LookupCAS(context.Background(), "run-1", digest, "other.md")
	if err != nil || ok {
		t.Fatalf("missing name should miss, ok=%v err=%v", ok, err)
	}
}
