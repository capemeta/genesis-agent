package skill

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"genesis-agent/internal/capabilities/skill/contract"
)

// WatcherOptions 控制本地 Skill watcher。
type WatcherOptions struct {
	PollInterval     time.Duration
	ThrottleInterval time.Duration
}

// Watcher 用轮询方式监听本地 skill roots。轮询避免引入平台特定 fsnotify 依赖。
type Watcher struct {
	opts WatcherOptions
}

// NewWatcher 创建本地 Skill watcher。
func NewWatcher(opts WatcherOptions) *Watcher {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	if opts.ThrottleInterval <= 0 {
		opts.ThrottleInterval = 500 * time.Millisecond
	}
	return &Watcher{opts: opts}
}

func (w *Watcher) Watch(ctx context.Context, roots []contract.WatchRoot) (<-chan contract.ChangeEvent, error) {
	out := make(chan contract.ChangeEvent, 1)
	initial := snapshotRoots(roots)
	var mu sync.Mutex
	lastEmit := time.Time{}
	go func() {
		defer close(out)
		ticker := time.NewTicker(w.opts.PollInterval)
		defer ticker.Stop()
		current := initial
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				next := snapshotRoots(roots)
				if snapshotsEqual(current, next) {
					continue
				}
				current = next
				mu.Lock()
				if !lastEmit.IsZero() && now.Sub(lastEmit) < w.opts.ThrottleInterval {
					mu.Unlock()
					continue
				}
				lastEmit = now
				mu.Unlock()
				select {
				case out <- contract.ChangeEvent{ChangedAt: now}:
				default:
				}
			}
		}
	}()
	return out, nil
}

type fileSnapshot map[string]time.Time

func snapshotRoots(roots []contract.WatchRoot) fileSnapshot {
	snapshot := make(fileSnapshot)
	for _, root := range roots {
		_ = filepath.WalkDir(root.Path, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() && !root.Recursive && path != root.Path {
				return filepath.SkipDir
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return nil
			}
			snapshot[path] = info.ModTime()
			return nil
		})
	}
	return snapshot
}

func snapshotsEqual(a, b fileSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	keys := make([]string, 0, len(a))
	for key := range a {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !a[key].Equal(b[key]) {
			return false
		}
	}
	return true
}
