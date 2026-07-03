package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tool "genesis-agent/internal/capabilities/tool/contract"
)

func TestQueueRunsReadOnlyConcurrentSiblingsTogether(t *testing.T) {
	queue := NewQueue()
	started := make(chan string, 2)
	release := make(chan struct{})
	tasks := []Task{
		blockingTask("a", true, started, release),
		blockingTask("b", true, started, release),
	}

	done := make(chan []Result, 1)
	go func() { done <- queue.Run(context.Background(), tasks) }()

	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case id := <-started:
			got[id] = true
		case <-time.After(time.Second):
			t.Fatalf("started = %v, want both read-only tasks", got)
		}
	}
	close(release)
	<-done
}

func TestQueueTreatsUnsafeTaskAsBarrier(t *testing.T) {
	queue := NewQueue()
	started := make(chan string, 2)
	releaseA := make(chan struct{})
	releaseB := make(chan struct{})
	tasks := []Task{
		blockingTask("a", false, started, releaseA),
		blockingTask("b", true, started, releaseB),
	}

	done := make(chan []Result, 1)
	go func() { done <- queue.Run(context.Background(), tasks) }()

	if got := waitStarted(t, started); got != "a" {
		t.Fatalf("first started = %s, want a", got)
	}
	select {
	case got := <-started:
		t.Fatalf("task %s started before barrier released", got)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseA)
	if got := waitStarted(t, started); got != "b" {
		t.Fatalf("second started = %s, want b", got)
	}
	close(releaseB)
	<-done
}

func TestQueueBoundsConcurrentSiblings(t *testing.T) {
	queue := NewQueue(QueueOptions{MaxConcurrency: 2})
	var active int32
	var maxActive int32
	tasks := make([]Task, 6)
	for i := range tasks {
		tasks[i] = Task{ID: "task", Name: "task", Traits: concurrentTraits(), Run: func(context.Context) (string, error) {
			current := atomic.AddInt32(&active, 1)
			for {
				seen := atomic.LoadInt32(&maxActive)
				if current <= seen || atomic.CompareAndSwapInt32(&maxActive, seen, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			return "ok", nil
		}}
	}

	results := queue.Run(context.Background(), tasks)
	for _, result := range results {
		if result.Err != nil {
			t.Fatalf("result error = %v", result.Err)
		}
	}
	if got := atomic.LoadInt32(&maxActive); got > 2 {
		t.Fatalf("max active = %d, want <= 2", got)
	}
}

func TestQueuePreservesResultOrder(t *testing.T) {
	queue := NewQueue()
	var mu sync.Mutex
	order := []string{}
	results := queue.Run(context.Background(), []Task{
		{ID: "1", Name: "one", Traits: concurrentTraits(), Run: func(context.Context) (string, error) {
			mu.Lock()
			order = append(order, "one")
			mu.Unlock()
			return "one", nil
		}},
		{ID: "2", Name: "two", Traits: concurrentTraits(), Run: func(context.Context) (string, error) {
			mu.Lock()
			order = append(order, "two")
			mu.Unlock()
			return "two", nil
		}},
	})
	if len(results) != 2 || results[0].Output != "one" || results[1].Output != "two" {
		t.Fatalf("results = %+v", results)
	}
}

func blockingTask(id string, concurrent bool, started chan<- string, release <-chan struct{}) Task {
	traits := tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: concurrent, ConcurrencySafe: concurrent}
	return Task{ID: id, Name: id, Traits: traits, Run: func(context.Context) (string, error) {
		started <- id
		<-release
		return id, nil
	}}
}

func concurrentTraits() tool.ToolTraits {
	return tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: true, ConcurrencySafe: true}
}

func waitStarted(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case got := <-started:
		return got
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task start")
		return ""
	}
}
