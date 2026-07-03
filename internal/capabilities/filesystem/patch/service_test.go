package patch

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/freshness"
	"genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

func TestServiceApplyMultipleOperations(t *testing.T) {
	backend := newMemoryBackend(map[string]string{
		"modify.txt": "line1\nline2\n",
		"delete.txt": "obsolete\n",
	})
	service, err := NewService(testDeps(backend))
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Apply(context.Background(), "*** Begin Patch\n*** Add File: nested/new.txt\n+created\n*** Delete File: delete.txt\n*** Update File: modify.txt\n@@\n-line2\n+changed\n*** End Patch")
	if err != nil {
		t.Fatal(err)
	}
	if got := backend.file("nested/new.txt"); got != "created\n" {
		t.Fatalf("added=%q", got)
	}
	if got := backend.file("modify.txt"); got != "line1\nchanged\n" {
		t.Fatalf("modified=%q", got)
	}
	if backend.exists("delete.txt") {
		t.Fatal("delete.txt still exists")
	}
	if !strings.Contains(result.Summary, "A nested/new.txt") || !strings.Contains(result.Summary, "M modify.txt") || !strings.Contains(result.Summary, "D delete.txt") {
		t.Fatalf("summary=%q", result.Summary)
	}
}

func TestServiceApplyMove(t *testing.T) {
	backend := newMemoryBackend(map[string]string{"old/name.txt": "old content\n"})
	service, err := NewService(testDeps(backend))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(context.Background(), "*** Begin Patch\n*** Update File: old/name.txt\n*** Move to: renamed/dir/name.txt\n@@\n-old content\n+new content\n*** End Patch")
	if err != nil {
		t.Fatal(err)
	}
	if backend.exists("old/name.txt") {
		t.Fatal("old file still exists")
	}
	if got := backend.file("renamed/dir/name.txt"); got != "new content\n" {
		t.Fatalf("moved=%q", got)
	}
}

type allowApproval struct{}

func (allowApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

type memoryResolver struct{}

func (memoryResolver) Resolve(_ context.Context, ref model.PathRef, opts fscontract.ResolveOptions) (model.ResolvedPath, error) {
	raw := strings.TrimPrefix(strings.ReplaceAll(ref.Raw, "\\", "/"), "./")
	if raw == "" {
		raw = "."
	}
	return model.ResolvedPath{DisplayPath: raw, BackendPath: raw, WorkspaceRel: raw, WorkspaceID: "test", Scope: model.PathScopeWorkspace, RawPath: ref.Raw}, nil
}

func testDeps(backend *memoryBackend) toolkit.Deps {
	return toolkit.Deps{Resolver: memoryResolver{}, Backend: backend, Approval: allowApproval{}, Freshness: freshness.NewMemoryTracker(), Locker: scheduler.NewMemoryResourceLocker()}
}

type memoryBackend struct {
	mu    sync.RWMutex
	files map[string]string
}

func newMemoryBackend(files map[string]string) *memoryBackend {
	copy := make(map[string]string, len(files))
	for k, v := range files {
		copy[k] = v
	}
	return &memoryBackend{files: copy}
}

func (b *memoryBackend) Read(_ context.Context, path model.ResolvedPath, _ fscontract.ReadOptions) ([]byte, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	v, ok := b.files[path.BackendPath]
	if !ok {
		return nil, fscontract.NewError(fscontract.ErrCodeNotFound, path.DisplayPath, fmt.Errorf("not found"))
	}
	return []byte(v), nil
}

func (b *memoryBackend) Write(_ context.Context, path model.ResolvedPath, content []byte, _ fscontract.WriteOptions) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.files[path.BackendPath] = string(content)
	return nil
}

func (b *memoryBackend) ListDir(context.Context, model.ResolvedPath, fscontract.ListOptions) ([]model.DirEntry, error) {
	return nil, nil
}

func (b *memoryBackend) Walk(context.Context, model.ResolvedPath, fscontract.WalkOptions) (*model.WalkOutcome, error) {
	return nil, nil
}

func (b *memoryBackend) Stat(_ context.Context, path model.ResolvedPath) (*model.FileStat, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if _, ok := b.files[path.BackendPath]; !ok {
		return nil, fscontract.NewError(fscontract.ErrCodeNotFound, path.DisplayPath, fmt.Errorf("not found"))
	}
	return &model.FileStat{Path: path, Type: model.EntryTypeFile, Size: int64(len(b.files[path.BackendPath])), ModifiedAt: time.Unix(1, 0)}, nil
}

func (b *memoryBackend) MkdirAll(context.Context, model.ResolvedPath, fscontract.MkdirOptions) error {
	return nil
}

func (b *memoryBackend) Remove(_ context.Context, path model.ResolvedPath, _ fscontract.RemoveOptions) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.files[path.BackendPath]; !ok {
		return fscontract.NewError(fscontract.ErrCodeNotFound, path.DisplayPath, fmt.Errorf("not found"))
	}
	delete(b.files, path.BackendPath)
	return nil
}

func (b *memoryBackend) file(path string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.files[path]
}

func (b *memoryBackend) exists(path string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.files[path]
	return ok
}

func (b *memoryBackend) sortedPaths() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	paths := make([]string, 0, len(b.files))
	for p := range b.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}
