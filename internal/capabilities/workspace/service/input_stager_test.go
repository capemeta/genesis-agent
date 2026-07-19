package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type fixedIDs struct{ values []string }

func (f *fixedIDs) Generate() string { value := f.values[0]; f.values = f.values[1:]; return value }

type memoryReader map[string][]byte

func (r memoryReader) Open(_ context.Context, ref workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	data := r[ref.ID]
	return workcontract.ResourceHandle{Reader: io.NopCloser(bytes.NewReader(data)), Size: int64(len(data)), Version: ref.Version}, nil
}

type countingReader struct {
	memoryReader
	opens int
}

func (r *countingReader) Open(ctx context.Context, ref workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	r.opens++
	return r.memoryReader.Open(ctx, ref)
}

type memoryStore struct {
	values map[workmodel.WorkspacePath][]byte
	puts   int
}

func (s *memoryStore) Put(_ context.Context, runID, inputID, name string, content io.Reader) (workmodel.WorkspacePath, error) {
	data, _ := io.ReadAll(content)
	p := workmodel.WorkspacePath("runs/" + runID + "/input/" + inputID + "/" + name)
	s.values[p] = data
	s.puts++
	return p, nil
}

func (s *memoryStore) PutCAS(_ context.Context, runID, name string, content io.Reader, maxBytes int64) (workcontract.PutCASResult, error) {
	limited := &io.LimitedReader{R: content, N: maxBytes + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return workcontract.PutCASResult{}, err
	}
	if int64(len(data)) > maxBytes {
		return workcontract.PutCASResult{}, workcontract.NewError(workcontract.ErrCodeInputTooLarge, io.ErrUnexpectedEOF)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	inputID := "cas-" + digest
	p := workmodel.WorkspacePath("runs/" + runID + "/input/" + inputID + "/" + name)
	if existing, ok := s.values[p]; ok {
		return workcontract.PutCASResult{Path: p, InputID: inputID, SHA256: digest, Size: int64(len(existing)), Reused: true}, nil
	}
	s.values[p] = data
	s.puts++
	return workcontract.PutCASResult{Path: p, InputID: inputID, SHA256: digest, Size: int64(len(data)), Reused: false}, nil
}

func (s *memoryStore) LookupCAS(_ context.Context, runID, sha256Hex, name string) (workcontract.PutCASResult, bool, error) {
	digest := strings.ToLower(strings.TrimSpace(sha256Hex))
	inputID := "cas-" + digest
	p := workmodel.WorkspacePath("runs/" + runID + "/input/" + inputID + "/" + name)
	existing, ok := s.values[p]
	if !ok {
		return workcontract.PutCASResult{}, false, nil
	}
	return workcontract.PutCASResult{Path: p, InputID: inputID, SHA256: digest, Size: int64(len(existing)), Reused: true}, true, nil
}

func (s *memoryStore) Remove(_ context.Context, p workmodel.WorkspacePath) error {
	delete(s.values, p)
	return nil
}

func TestInputStagerRenamesDuplicatesAndHashes(t *testing.T) {
	store := &memoryStore{values: map[workmodel.WorkspacePath][]byte{}}
	stager, err := NewInputStager(memoryReader{"a": []byte("first"), "b": []byte("second")}, store, &fixedIDs{values: []string{"1", "2"}})
	if err != nil {
		t.Fatal(err)
	}
	binding := execmodel.ExecutionBinding{ID: "binding-1", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1"}}
	manifest, err := stager.Stage(context.Background(), workcontract.StageRequest{Binding: binding, Sources: []workmodel.ResourceRef{
		{Authority: "host", Scheme: "file", ID: "a", Path: "folder/report.pdf"},
		{Authority: "host", Scheme: "file", ID: "b", Path: "other/report.pdf"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Inputs) != 2 || manifest.Inputs[0].Alias != "folder/report.pdf" || manifest.Inputs[1].Alias != "other/report.pdf" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if manifest.Inputs[0].SHA256 == "" || manifest.Inputs[0].StagedPath == manifest.Inputs[1].StagedPath {
		t.Fatalf("invalid immutable inputs: %+v", manifest.Inputs)
	}
}

func TestInputStagerRemovesOversizedSnapshot(t *testing.T) {
	store := &memoryStore{values: map[workmodel.WorkspacePath][]byte{}}
	stager, _ := NewInputStager(memoryReader{"a": []byte("too-large")}, store, &fixedIDs{values: []string{"1"}})
	binding := execmodel.ExecutionBinding{ID: "binding-1", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run-1"}}
	_, err := stager.Stage(context.Background(), workcontract.StageRequest{Binding: binding, Sources: []workmodel.ResourceRef{{Authority: "host", Scheme: "file", ID: "a", Path: "a.txt"}}, MaxFileSize: 3})
	if err == nil {
		t.Fatal("Stage() error = nil")
	}
	if len(store.values) != 0 {
		t.Fatalf("oversized snapshot leaked: %+v", store.values)
	}
}

func TestInputStagerReusesSameContentAndAliasAcrossStages(t *testing.T) {
	store := &memoryStore{values: map[workmodel.WorkspacePath][]byte{}}
	reader := memoryReader{"doc": []byte("same-bytes")}
	stager, err := NewInputStager(reader, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding := execmodel.ExecutionBinding{
		ID: "binding-1", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite,
		Owner: execmodel.ExecutionOwnerRef{TenantID: "t", ProjectID: "p", UserID: "u", RunID: "run-1"},
	}
	source := workmodel.ResourceRef{Authority: "host", Scheme: "file", ID: "doc", Path: "ultra5.md", Scope: workmodel.ResourceScope{TenantID: "t", ProjectID: "p", UserID: "u"}}
	first, err := stager.Stage(context.Background(), workcontract.StageRequest{Binding: binding, Sources: []workmodel.ResourceRef{source}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := stager.Stage(context.Background(), workcontract.StageRequest{Binding: binding, Sources: []workmodel.ResourceRef{source}})
	if err != nil {
		t.Fatal(err)
	}
	if store.puts != 1 {
		t.Fatalf("expected single physical put, puts=%d values=%d", store.puts, len(store.values))
	}
	if first.Inputs[0].StagedPath != second.Inputs[0].StagedPath {
		t.Fatalf("staged path not reused: %q vs %q", first.Inputs[0].StagedPath, second.Inputs[0].StagedPath)
	}
	if first.Inputs[0].SHA256 != second.Inputs[0].SHA256 || first.Inputs[0].ID != second.Inputs[0].ID {
		t.Fatalf("cas identity mismatch: %+v vs %+v", first.Inputs[0], second.Inputs[0])
	}
}

func TestInputStagerSkipsPutCASWhenTrustedDigestHitsLookup(t *testing.T) {
	content := []byte("same-bytes")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	store := &memoryStore{values: map[workmodel.WorkspacePath][]byte{}}
	reader := &countingReader{memoryReader: memoryReader{"doc": content}}
	stager, err := NewInputStager(reader, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding := execmodel.ExecutionBinding{
		ID: "binding-1", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite,
		Owner: execmodel.ExecutionOwnerRef{TenantID: "t", ProjectID: "p", UserID: "u", RunID: "run-1"},
	}
	source := workmodel.ResourceRef{
		Authority: "host", Scheme: "file", ID: "doc", Path: "ultra5.md",
		Version: "sha256:" + digest,
		Scope:   workmodel.ResourceScope{TenantID: "t", ProjectID: "p", UserID: "u"},
	}
	if _, err := stager.Stage(context.Background(), workcontract.StageRequest{Binding: binding, Sources: []workmodel.ResourceRef{source}}); err != nil {
		t.Fatal(err)
	}
	if store.puts != 1 || reader.opens != 1 {
		t.Fatalf("first stage puts=%d opens=%d", store.puts, reader.opens)
	}
	if _, err := stager.Stage(context.Background(), workcontract.StageRequest{Binding: binding, Sources: []workmodel.ResourceRef{source}}); err != nil {
		t.Fatal(err)
	}
	if store.puts != 1 {
		t.Fatalf("trusted digest should skip PutCAS, puts=%d", store.puts)
	}
	if reader.opens != 2 {
		t.Fatalf("still must Open for auth/mutation check, opens=%d", reader.opens)
	}
}

type sizedMemoryReader struct {
	data  map[string][]byte
	sizes map[string]int64
}

func (r sizedMemoryReader) Open(_ context.Context, ref workmodel.ResourceRef) (workcontract.ResourceHandle, error) {
	data := r.data[ref.ID]
	size := int64(len(data))
	if override, ok := r.sizes[ref.ID]; ok {
		size = override
	}
	return workcontract.ResourceHandle{Reader: io.NopCloser(bytes.NewReader(data)), Size: size}, nil
}

func TestInputStagerRejectsWhenRemainingTotalExhausted(t *testing.T) {
	store := &memoryStore{values: map[workmodel.WorkspacePath][]byte{}}
	// 第二份 Size=-1，绕过预检，覆盖 PutCAS 前 limit<=0 守卫。
	stager, err := NewInputStager(sizedMemoryReader{
		data:  map[string][]byte{"a": []byte("1234"), "b": []byte("x")},
		sizes: map[string]int64{"b": -1},
	}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding := execmodel.ExecutionBinding{
		ID: "binding-1", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite,
		Owner: execmodel.ExecutionOwnerRef{TenantID: "t", ProjectID: "p", UserID: "u", RunID: "run-1"},
	}
	scope := workmodel.ResourceScope{TenantID: "t", ProjectID: "p", UserID: "u"}
	_, err = stager.Stage(context.Background(), workcontract.StageRequest{
		Binding: binding,
		Sources: []workmodel.ResourceRef{
			{Authority: "host", Scheme: "file", ID: "a", Path: "a.txt", Scope: scope},
			{Authority: "host", Scheme: "file", ID: "b", Path: "b.txt", Scope: scope},
		},
		MaxFileSize: 10,
		MaxTotal:    4,
	})
	if err == nil {
		t.Fatal("expected total limit error")
	}
	// Stage 失败整批回滚：本次新建的快照应被清除。
	if len(store.values) != 0 {
		t.Fatalf("failed stage should rollback created snapshots, values=%d", len(store.values))
	}
}
