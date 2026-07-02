// Package scheduler 提供工具资源锁和后续调度器基础模型。
package scheduler

import (
	"context"
	"sort"
	"sync"
	"time"
)

const lockRetryInterval = 10 * time.Millisecond

// LockMode 描述资源锁模式。
type LockMode string

const (
	LockRead  LockMode = "read"
	LockWrite LockMode = "write"
)

// ResourceLock 描述工具执行需要占用的资源。
type ResourceLock struct {
	Scope string
	Key   string
	Mode  LockMode
}

// ResourceLocker 是 session/run scoped 资源锁。
type ResourceLocker interface {
	Acquire(ctx context.Context, locks []ResourceLock) (func(), error)
}

type namedLock struct {
	mu sync.RWMutex
}

// MemoryResourceLocker 是进程内资源锁实现。
type MemoryResourceLocker struct {
	mu    sync.Mutex
	locks map[string]*namedLock
}

// NewMemoryResourceLocker 创建资源锁。
func NewMemoryResourceLocker() *MemoryResourceLocker {
	return &MemoryResourceLocker{locks: make(map[string]*namedLock)}
}

// Acquire 按稳定顺序获取锁，避免死锁。
func (l *MemoryResourceLocker) Acquire(ctx context.Context, locks []ResourceLock) (func(), error) {
	ordered := normalizeLocks(locks)
	acquired := make([]ResourceLock, 0, len(ordered))
	for _, lock := range ordered {
		if err := ctx.Err(); err != nil {
			release(l, acquired)
			return nil, err
		}
		named := l.get(lock)
		if err := lockWithContext(ctx, named, lock.Mode); err != nil {
			release(l, acquired)
			return nil, err
		}
		acquired = append(acquired, lock)
	}
	return func() { release(l, acquired) }, nil
}

func lockWithContext(ctx context.Context, named *namedLock, mode LockMode) error {
	for {
		if mode == LockWrite {
			if named.mu.TryLock() {
				return nil
			}
		} else if named.mu.TryRLock() {
			return nil
		}

		timer := time.NewTimer(lockRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *MemoryResourceLocker) get(lock ResourceLock) *namedLock {
	name := lock.Scope + ":" + lock.Key
	l.mu.Lock()
	defer l.mu.Unlock()
	named := l.locks[name]
	if named == nil {
		named = &namedLock{}
		l.locks[name] = named
	}
	return named
}

func release(l *MemoryResourceLocker, acquired []ResourceLock) {
	for i := len(acquired) - 1; i >= 0; i-- {
		lock := acquired[i]
		named := l.get(lock)
		if lock.Mode == LockWrite {
			named.mu.Unlock()
		} else {
			named.mu.RUnlock()
		}
	}
}

func normalizeLocks(locks []ResourceLock) []ResourceLock {
	merged := make(map[string]ResourceLock)
	for _, lock := range locks {
		if lock.Scope == "" || lock.Key == "" {
			continue
		}
		if lock.Mode == "" {
			lock.Mode = LockRead
		}
		name := lock.Scope + ":" + lock.Key
		existing, ok := merged[name]
		if !ok || existing.Mode == LockRead && lock.Mode == LockWrite {
			merged[name] = lock
		}
	}
	ordered := make([]ResourceLock, 0, len(merged))
	for _, lock := range merged {
		ordered = append(ordered, lock)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left := ordered[i].Scope + ":" + ordered[i].Key
		right := ordered[j].Scope + ":" + ordered[j].Key
		return left < right
	})
	return ordered
}
