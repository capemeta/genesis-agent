package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestMemoryResourceLockerReadReadDoesNotBlock(t *testing.T) {
	locker := NewMemoryResourceLocker()
	release1, err := locker.Acquire(context.Background(), []ResourceLock{{Scope: "file", Key: "a", Mode: LockRead}})
	if err != nil {
		t.Fatal(err)
	}
	defer release1()

	done := make(chan struct{})
	go func() {
		release2, err := locker.Acquire(context.Background(), []ResourceLock{{Scope: "file", Key: "a", Mode: LockRead}})
		if err != nil {
			t.Error(err)
			close(done)
			return
		}
		release2()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second read lock blocked")
	}
}

func TestMemoryResourceLockerWriteBlocksRead(t *testing.T) {
	locker := NewMemoryResourceLocker()
	release, err := locker.Acquire(context.Background(), []ResourceLock{{Scope: "file", Key: "a", Mode: LockWrite}})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		release2, err := locker.Acquire(context.Background(), []ResourceLock{{Scope: "file", Key: "a", Mode: LockRead}})
		if err != nil {
			t.Error(err)
			close(done)
			return
		}
		release2()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("read lock acquired while write lock held")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("read lock did not acquire after write release")
	}
}

func TestMemoryResourceLockerAcquireHonorsContextCancel(t *testing.T) {
	locker := NewMemoryResourceLocker()
	release, err := locker.Acquire(context.Background(), []ResourceLock{{Scope: "file", Key: "a", Mode: LockWrite}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = locker.Acquire(ctx, []ResourceLock{{Scope: "file", Key: "a", Mode: LockRead}})
	if err == nil {
		t.Fatal("Acquire error = nil, want context timeout")
	}
}
