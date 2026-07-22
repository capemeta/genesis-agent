package view_image

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/freshness"
	"genesis-agent/internal/capabilities/filesystem/model"
	"genesis-agent/internal/capabilities/filesystem/tool/toolkit"
	"genesis-agent/internal/capabilities/llm/vision"
	"genesis-agent/internal/capabilities/tool/scheduler"
)

type allowApproval struct{}

func (allowApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved}, nil
}

type memoryResolver struct{}

func (memoryResolver) Resolve(_ context.Context, ref model.PathRef, _ fscontract.ResolveOptions) (model.ResolvedPath, error) {
	raw := strings.TrimPrefix(strings.ReplaceAll(ref.Raw, "\\", "/"), "./")
	return model.ResolvedPath{DisplayPath: raw, BackendPath: raw, WorkspaceRel: raw, WorkspaceID: "test", Scope: model.PathScopeWorkspace, RawPath: ref.Raw}, nil
}

type memoryBackend struct {
	mu    sync.RWMutex
	files map[string][]byte
}

func (b *memoryBackend) Read(_ context.Context, path model.ResolvedPath, _ fscontract.ReadOptions) ([]byte, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	v, ok := b.files[path.BackendPath]
	if !ok {
		return nil, fscontract.NewError(fscontract.ErrCodeNotFound, path.DisplayPath, fmt.Errorf("not found"))
	}
	return v, nil
}
func (b *memoryBackend) Write(context.Context, model.ResolvedPath, []byte, fscontract.WriteOptions) error {
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
	v, ok := b.files[path.BackendPath]
	if !ok {
		return nil, fscontract.NewError(fscontract.ErrCodeNotFound, path.DisplayPath, fmt.Errorf("not found"))
	}
	return &model.FileStat{Path: path, Type: model.EntryTypeFile, Size: int64(len(v)), ModifiedAt: time.Unix(1, 0)}, nil
}
func (b *memoryBackend) MkdirAll(context.Context, model.ResolvedPath, fscontract.MkdirOptions) error {
	return nil
}
func (b *memoryBackend) Remove(context.Context, model.ResolvedPath, fscontract.RemoveOptions) error {
	return nil
}

func testDeps(files map[string][]byte) toolkit.Deps {
	return toolkit.Deps{
		Resolver:  memoryResolver{},
		Backend:   &memoryBackend{files: files},
		Approval:  allowApproval{},
		Freshness: freshness.NewMemoryTracker(),
		Locker:    scheduler.NewMemoryResourceLocker(),
	}
}

func TestViewImageSupportsAbsoluteHostPathAndRejectsNonImage(t *testing.T) {
	t.Parallel()
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 1, 2, 3}
	tool, err := New(testDeps(map[string][]byte{"D:/abs/a.png": png, "a.docx": []byte("not-image")}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithVisionMode(context.Background(), vision.ModeDirectInject)
	out, err := tool.Execute(ctx, `{"path":"D:/abs/a.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"ok": true`) || !strings.Contains(out, "D:/abs/a.png") {
		t.Fatalf("want ok true for absolute path, got %s", out)
	}
	out, err = tool.Execute(ctx, `{"path":"a.docx"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, errNotAnImage) {
		t.Fatalf("want not_an_image, got %s", out)
	}
}

func TestViewImagePNGAndDegraded(t *testing.T) {
	t.Parallel()
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 1, 2, 3}
	tool, err := New(testDeps(map[string][]byte{"slide.png": png}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithVisionMode(context.Background(), vision.ModeDirectInject)
	out, err := tool.Execute(ctx, `{"path":"slide.png","detail":"high"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"ok": true`) || !strings.Contains(out, "slide.png") {
		t.Fatalf("got %s", out)
	}
	deg := WithVisionMode(context.Background(), vision.ModeDegradedText)
	out, err = tool.Execute(deg, `{"path":"slide.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, errVisionUnavailable) {
		t.Fatalf("got %s", out)
	}
	if !strings.Contains(out, "Pillow") || !strings.Contains(out, "Do NOT") {
		t.Fatalf("degraded message should forbid pseudo-vision: %s", out)
	}
}
