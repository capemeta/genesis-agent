package visionio

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireCap3(t *testing.T) {
	t.Cleanup(func() { SetMaxConcurrent(DefaultMaxConcurrentReads) })
	SetMaxConcurrent(3)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := Acquire(ctx); err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}
	blocked := make(chan error, 1)
	go func() {
		c, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		blocked <- Acquire(c)
	}()
	if err := <-blocked; err == nil {
		t.Fatal("4th acquire should block/timeout")
	}
	Release()
	if err := Acquire(ctx); err != nil {
		t.Fatalf("after release: %v", err)
	}
	Release()
	Release()
	Release()
}

func TestAcquireParallel(t *testing.T) {
	t.Cleanup(func() { SetMaxConcurrent(DefaultMaxConcurrentReads) })
	SetMaxConcurrent(3)
	var peak atomic.Int32
	var cur atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Acquire(context.Background())
			n := cur.Add(1)
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			cur.Add(-1)
			Release()
		}()
	}
	wg.Wait()
	if peak.Load() > 3 {
		t.Fatalf("peak concurrency=%d want <=3", peak.Load())
	}
}
