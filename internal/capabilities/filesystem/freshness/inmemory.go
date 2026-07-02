package freshness

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"time"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	"genesis-agent/internal/capabilities/filesystem/model"
)

type record struct {
	modifiedAt time.Time
	size       int64
	hash       string
}

// MemoryTracker 是 session scoped 新鲜度追踪器。
type MemoryTracker struct {
	mu      sync.RWMutex
	records map[string]record
}

// NewMemoryTracker 创建内存新鲜度追踪器。
func NewMemoryTracker() *MemoryTracker {
	return &MemoryTracker{records: make(map[string]record)}
}

func (t *MemoryTracker) RecordRead(_ context.Context, path model.ResolvedPath, stat model.FileStat, hash string) error {
	t.set(path, stat, hash)
	return nil
}

func (t *MemoryTracker) CheckBeforeWrite(_ context.Context, path model.ResolvedPath, current model.FileStat, currentHash string, expectedHash string) (*Check, error) {
	if expectedHash != "" && currentHash != "" && expectedHash != currentHash {
		return nil, fscontract.NewError(fscontract.ErrCodeModifiedExternally, path.DisplayPath, errModified)
	}
	if expectedHash != "" {
		return &Check{Fresh: true, Reason: "expected hash matched"}, nil
	}

	t.mu.RLock()
	rec, ok := t.records[key(path)]
	t.mu.RUnlock()
	if !ok {
		return nil, fscontract.NewError(fscontract.ErrCodeModifiedExternally, path.DisplayPath, simpleError("file has not been read in this session; read it first or provide expected_hash"))
	}
	if rec.hash != "" && currentHash != "" {
		if rec.hash != currentHash {
			return nil, fscontract.NewError(fscontract.ErrCodeModifiedExternally, path.DisplayPath, errModified)
		}
		return &Check{Fresh: true, Reason: "hash matched"}, nil
	}
	if !rec.modifiedAt.Equal(current.ModifiedAt) || rec.size != current.Size {
		return nil, fscontract.NewError(fscontract.ErrCodeModifiedExternally, path.DisplayPath, errModified)
	}
	return &Check{Fresh: true, Reason: "mtime and size matched"}, nil
}

func (t *MemoryTracker) RecordWrite(_ context.Context, path model.ResolvedPath, stat model.FileStat, hash string) error {
	t.set(path, stat, hash)
	return nil
}

func (t *MemoryTracker) set(path model.ResolvedPath, stat model.FileStat, hash string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records[key(path)] = record{modifiedAt: stat.ModifiedAt, size: stat.Size, hash: hash}
}

func key(path model.ResolvedPath) string {
	value := path.BackendPath
	if value == "" {
		value = path.WorkspaceRel
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(value)
	}
	return value
}

var errModified = simpleError("file modified externally")

type simpleError string

func (e simpleError) Error() string { return string(e) }
