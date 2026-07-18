package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type fixedLocatorIDs struct{ value string }

func (g fixedLocatorIDs) Generate() string { return g.value }

type fakeRemoteFiles struct {
	content []byte
	modTime time.Time
}

func (f *fakeRemoteFiles) ReadFile(context.Context, sandboxcontract.FileRequest, fscontract.ReadOptions) ([]byte, error) {
	return append([]byte(nil), f.content...), nil
}
func (f *fakeRemoteFiles) Stat(_ context.Context, req sandboxcontract.FileRequest) (*fsmodel.FileStat, error) {
	digest := sha256.Sum256(f.content)
	return &fsmodel.FileStat{Path: req.Path, Type: fsmodel.EntryTypeFile, Size: int64(len(f.content)), ModifiedAt: f.modTime, Hash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}
func (*fakeRemoteFiles) WriteFile(context.Context, sandboxcontract.WriteFileRequest) error {
	return nil
}
func (*fakeRemoteFiles) ListDir(context.Context, sandboxcontract.ListDirRequest) ([]fsmodel.DirEntry, error) {
	return nil, nil
}
func (*fakeRemoteFiles) Walk(context.Context, sandboxcontract.WalkRequest) (*fsmodel.WalkOutcome, error) {
	return nil, nil
}
func (*fakeRemoteFiles) MkdirAll(context.Context, sandboxcontract.MkdirRequest) error { return nil }
func (*fakeRemoteFiles) Remove(context.Context, sandboxcontract.RemoveRequest) error  { return nil }

func TestSessionFileResolverPersistsPureLocatorAndReaderReopensIt(t *testing.T) {
	ctx := context.Background()
	files := &fakeRemoteFiles{content: []byte("remote ppt"), modTime: time.Now().UTC()}
	store, err := NewFileRemoteLocatorStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour)
	binding := testRemoteBinding()
	sessions, _ := NewFileSessionBindingStore(t.TempDir())
	if err := sessions.BindSessionExecution(ctx, SessionExecutionBinding{TenantID: "tenant", RunID: "run", BindingID: binding.ID, Workspace: sandboxcontract.WorkspaceRef{ID: "session-1", Provider: "genesis-sandbox", Metadata: map[string]string{"session_id": "session-1", "credential": "must-not-persist"}}, ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	resolver, _ := NewSessionFileResolver(files, sessions, store, fixedLocatorIDs{"one"})
	ref, err := resolver.ResolveProducedResource(ctx, workcontract.BackendResourceRequest{
		TenantID: "tenant", RunID: "run", Execution: workmodel.PreparedExecutionSnapshot{Binding: binding, Backend: testRemoteBackend()},
		ObservedPath: "work/binding/output.pptx", ObservedName: "output.pptx", Size: int64(len(files.content)), Availability: workmodel.ResourceAvailabilityLeased, ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Authority != RemoteExecutorAuthority || ref.Scheme != SessionFileScheme || strings.Contains(ref.ID, "session-1") {
		t.Fatalf("ref leaked locator details: %+v", ref)
	}
	locator, err := store.Get(ctx, ref.ID, ref.Scope)
	if err != nil || locator.Workspace.Metadata["credential"] != "" {
		t.Fatalf("locator leaked workspace metadata: %+v err=%v", locator.Workspace.Metadata, err)
	}
	reader, _ := NewSessionFileReader(files, store)
	handle, err := reader.Open(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Reader.Close()
	data, err := io.ReadAll(handle.Reader)
	if err != nil || string(data) != "remote ppt" || handle.Version != ref.Version {
		t.Fatalf("data=%q version=%q err=%v", data, handle.Version, err)
	}
}

func TestSessionFileResolverReusesPreferSourceWithoutOrphanLocator(t *testing.T) {
	ctx := context.Background()
	files := &fakeRemoteFiles{content: []byte("remote ppt"), modTime: time.Now().UTC()}
	store, err := NewFileRemoteLocatorStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionExpires := time.Now().Add(2 * time.Hour)
	expires := time.Now().Add(time.Hour)
	binding := testRemoteBinding()
	sessions, _ := NewFileSessionBindingStore(t.TempDir())
	if err := sessions.BindSessionExecution(ctx, SessionExecutionBinding{TenantID: "tenant", RunID: "run", BindingID: binding.ID, Workspace: sandboxcontract.WorkspaceRef{ID: "session-1", Provider: "genesis-sandbox", Metadata: map[string]string{"session_id": "session-1"}}, ExpiresAt: sessionExpires}); err != nil {
		t.Fatal(err)
	}
	resolver, _ := NewSessionFileResolver(files, sessions, store, &sequentialLocatorIDs{values: []string{"one", "two"}})
	req := workcontract.BackendResourceRequest{
		TenantID: "tenant", RunID: "run", Execution: workmodel.PreparedExecutionSnapshot{Binding: binding, Backend: testRemoteBackend()},
		ObservedPath: "work/binding/output.pptx", ObservedName: "output.pptx", Size: int64(len(files.content)), Availability: workmodel.ResourceAvailabilityLeased, ExpiresAt: &expires,
	}
	first, err := resolver.ResolveProducedResource(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	extended := expires.Add(30 * time.Minute)
	req.ExpiresAt = &extended
	req.PreferSource = &first
	second, err := resolver.ResolveProducedResource(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Version != first.Version {
		t.Fatalf("prefer source should reuse locator: first=%+v second=%+v", first, second)
	}
	locator, err := store.Get(ctx, first.ID, first.Scope)
	if err != nil || locator.ExpiresAt == nil || !locator.ExpiresAt.Equal(extended) {
		t.Fatalf("lease should refresh on reuse: %+v err=%v", locator, err)
	}
	entries, err := os.ReadDir(store.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("must not create orphan locator files: %d", len(entries))
	}
}

type sequentialLocatorIDs struct{ values []string }

func (g *sequentialLocatorIDs) Generate() string {
	if len(g.values) == 0 {
		return "overflow"
	}
	v := g.values[0]
	g.values = g.values[1:]
	return v
}

func TestSessionFileReaderPreservesExpiryAndVersionErrors(t *testing.T) {
	ctx := context.Background()
	content := []byte("v1")
	digest := sha256.Sum256(content)
	store, _ := NewFileRemoteLocatorStore(t.TempDir())
	expired := time.Now().Add(-time.Minute)
	locator := RemoteLocator{ID: "expired", Authority: RemoteExecutorAuthority, Scheme: SessionFileScheme, Workspace: sandboxcontract.WorkspaceRef{ID: "session-1"}, Path: "work/out.txt", Scope: workmodel.ResourceScope{TenantID: "tenant"}, Version: "sha256:" + hex.EncodeToString(digest[:]), Size: int64(len(content)), Availability: workmodel.ResourceAvailabilityLeased, ExpiresAt: &expired}
	if err := store.Create(ctx, locator); err != nil {
		t.Fatal(err)
	}
	reader, _ := NewSessionFileReader(&fakeRemoteFiles{content: content, modTime: time.Now()}, store)
	ref := workmodel.ResourceRef{Authority: RemoteExecutorAuthority, Scheme: SessionFileScheme, ID: locator.ID, Version: locator.Version, Scope: locator.Scope}
	if _, err := reader.Open(ctx, ref); !hasWorkspaceError(err, workcontract.ErrCodeProducedResourceExpired) {
		t.Fatalf("expiry err=%v", err)
	}
	ref.Version = "sha256:different"
	if _, err := reader.Open(ctx, ref); !hasWorkspaceError(err, workcontract.ErrCodeProducedResourceVersionConflict) {
		t.Fatalf("version err=%v", err)
	}
}

func TestSessionBindingStoreIsExclusiveIdempotentAndResolverUsesBinding(t *testing.T) {
	ctx := context.Background()
	sessions, _ := NewFileSessionBindingStore(t.TempDir())
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	first := SessionExecutionBinding{TenantID: "tenant", RunID: "run", BindingID: "binding-a", Workspace: sandboxcontract.WorkspaceRef{ID: "session-a", Provider: "genesis-sandbox"}, ExpiresAt: expires}
	if err := sessions.BindSessionExecution(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := sessions.BindSessionExecution(ctx, first); err != nil {
		t.Fatalf("identical bind must be idempotent: %v", err)
	}
	extended := first
	extended.ExpiresAt = expires.Add(time.Hour)
	if err := sessions.BindSessionExecution(ctx, extended); err != nil {
		t.Fatalf("same workspace lease extend must succeed: %v", err)
	}
	gotExt, err := sessions.GetSessionExecution(ctx, "tenant", "run", "binding-a")
	if err != nil || !gotExt.ExpiresAt.Equal(extended.ExpiresAt) {
		t.Fatalf("extended binding=%+v err=%v", gotExt, err)
	}
	conflict := first
	conflict.Workspace.ID = "session-other"
	if err := sessions.BindSessionExecution(ctx, conflict); !hasWorkspaceError(err, workcontract.ErrCodeProducedResourceBackendMismatch) {
		t.Fatalf("conflicting bind err=%v", err)
	}
	second := SessionExecutionBinding{TenantID: "tenant", RunID: "run", BindingID: "binding-b", Workspace: sandboxcontract.WorkspaceRef{ID: "session-b", Provider: "genesis-sandbox"}, ExpiresAt: expires}
	if err := sessions.BindSessionExecution(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, err := sessions.GetSessionExecution(ctx, "tenant", "run", "binding-b")
	if err != nil || got.Workspace.ID != "session-b" {
		t.Fatalf("dynamic binding=%+v err=%v", got, err)
	}
}

type fakeObjectClient struct{ content []byte }

func (c fakeObjectClient) OpenObject(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(c.content))), nil
}

func TestExecutorObjectResolverAndReaderVerifyStreamAtEOF(t *testing.T) {
	ctx := context.Background()
	store, _ := NewFileRemoteLocatorStore(t.TempDir())
	resolver, _ := NewExecutorObjectResolver(store, fixedLocatorIDs{"object"})
	content := []byte("durable")
	digest := sha256.Sum256(content)
	version := "sha256:" + hex.EncodeToString(digest[:])
	ref, err := resolver.Resolve(ctx, testRemoteBackend(), workmodel.ResourceScope{TenantID: "tenant"}, execmodel.ExecutorOutputObject{ID: "executor-object-9", Name: "out.bin", Size: int64(len(content)), SHA256: strings.TrimPrefix(version, "sha256:"), Version: version, Availability: string(workmodel.ResourceAvailabilityDurable)})
	if err != nil {
		t.Fatal(err)
	}
	reader, _ := NewExecutorObjectReader(fakeObjectClient{content: []byte("tampered")}, store)
	handle, err := reader.Open(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(handle.Reader)
	_ = handle.Reader.Close()
	if !hasWorkspaceError(err, workcontract.ErrCodeProducedResourceVersionConflict) {
		t.Fatalf("stream version err=%v", err)
	}
}

func TestRemoteReaderRejectsBackendSchemeAndLocatorScope(t *testing.T) {
	store, _ := NewFileRemoteLocatorStore(t.TempDir())
	reader, _ := NewSessionFileReader(&fakeRemoteFiles{}, store)
	ref := workmodel.ResourceRef{Authority: "host", Scheme: SessionFileScheme, ID: "x", Version: "v", Scope: workmodel.ResourceScope{TenantID: "tenant"}}
	if _, err := reader.Open(context.Background(), ref); !hasWorkspaceError(err, workcontract.ErrCodeProducedResourceBackendMismatch) {
		t.Fatalf("backend err=%v", err)
	}
}

func testRemoteBackend() execmodel.ExecutionBackendRef {
	return execmodel.ExecutionBackendRef{Kind: execmodel.BackendKindRemote, Provider: "genesis-sandbox", InstanceID: "session-1", Authority: RemoteExecutorAuthority}
}

func testRemoteBinding() execmodel.ExecutionBinding {
	return execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{TenantID: "tenant", RunID: "run", AgentAppID: "app", AgentAppVersion: "1"}}
}

func hasWorkspaceError(err error, code workcontract.ErrorCode) bool {
	var target *workcontract.Error
	return errors.As(err, &target) && target.Code == code
}

var _ sandboxcontract.FileSystemClient = (*fakeRemoteFiles)(nil)
