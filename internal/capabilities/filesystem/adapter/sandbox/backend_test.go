package sandbox

import (
	"context"
	"testing"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
)

type fakeFileSystemClient struct {
	listRequest sandboxcontract.ListDirRequest
	entries     []fsmodel.DirEntry
}

func (f *fakeFileSystemClient) ReadFile(context.Context, sandboxcontract.FileRequest, fscontract.ReadOptions) ([]byte, error) {
	return nil, nil
}
func (f *fakeFileSystemClient) WriteFile(context.Context, sandboxcontract.WriteFileRequest) error {
	return nil
}
func (f *fakeFileSystemClient) ListDir(_ context.Context, req sandboxcontract.ListDirRequest) ([]fsmodel.DirEntry, error) {
	f.listRequest = req
	return f.entries, nil
}
func (f *fakeFileSystemClient) Walk(context.Context, sandboxcontract.WalkRequest) (*fsmodel.WalkOutcome, error) {
	return nil, nil
}
func (f *fakeFileSystemClient) Stat(context.Context, sandboxcontract.FileRequest) (*fsmodel.FileStat, error) {
	return nil, nil
}
func (f *fakeFileSystemClient) MkdirAll(context.Context, sandboxcontract.MkdirRequest) error {
	return nil
}
func (f *fakeFileSystemClient) Remove(context.Context, sandboxcontract.RemoveRequest) error {
	return nil
}

func TestBackendListDirAdaptsEntryTypeWithoutChangingRemoteOptions(t *testing.T) {
	client := &fakeFileSystemClient{entries: []fsmodel.DirEntry{
		{Name: "a.txt", Type: fsmodel.EntryTypeFile},
		{Name: "folder", Type: fsmodel.EntryTypeDir},
	}}
	backend, err := NewBackend(client, sandboxcontract.WorkspaceRef{ID: "ws"})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := backend.ListDir(context.Background(), fsmodel.ResolvedPath{DisplayPath: "."}, fscontract.ListOptions{
		MaxEntries: 1,
		EntryType:  fsmodel.EntryTypeDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.listRequest.Options.EntryType != "" || client.listRequest.Options.MaxEntries != 0 {
		t.Fatalf("remote options = %+v", client.listRequest.Options)
	}
	if len(entries) != 1 || entries[0].Name != "folder" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestBackendListDirRejectsInvalidEntryType(t *testing.T) {
	backend, err := NewBackend(&fakeFileSystemClient{}, sandboxcontract.WorkspaceRef{ID: "ws"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.ListDir(context.Background(), fsmodel.ResolvedPath{DisplayPath: "."}, fscontract.ListOptions{EntryType: "directory"})
	if fscontract.CodeOf(err) != fscontract.ErrCodeInvalidInput {
		t.Fatalf("CodeOf(err) = %q", fscontract.CodeOf(err))
	}
}
