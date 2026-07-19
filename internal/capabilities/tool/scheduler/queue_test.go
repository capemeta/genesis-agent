package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tool "genesis-agent/internal/capabilities/tool/contract"
)

func TestQueueRunsConcurrencySafeSiblingsTogether(t *testing.T) {
	queue := NewQueue()
	started := make(chan string, 2)
	release := make(chan struct{})
	tasks := []Task{
		blockingTask("a", concurrentTraits(), started, release),
		blockingTask("b", concurrentTraits(), started, release),
	}

	done := make(chan []Result, 1)
	go func() { done <- queue.Run(context.Background(), tasks) }()

	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case id := <-started:
			got[id] = true
		case <-time.After(time.Second):
			t.Fatalf("started = %v, want both concurrency-safe tasks", got)
		}
	}
	close(release)
	<-done
}

func TestQueueAllowsParallelWhenConcurrencySafeEvenIfNotReadOnly(t *testing.T) {
	queue := NewQueue()
	started := make(chan string, 2)
	release := make(chan struct{})
	traits := tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: true}
	tasks := []Task{
		blockingTask("task-a", traits, started, release),
		blockingTask("task-b", traits, started, release),
	}

	done := make(chan []Result, 1)
	go func() { done <- queue.Run(context.Background(), tasks) }()

	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case id := <-started:
			got[id] = true
		case <-time.After(time.Second):
			t.Fatalf("started = %v, want both Task-like siblings in parallel", got)
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
		blockingTask("a", barrierTraits(), started, releaseA),
		blockingTask("b", concurrentTraits(), started, releaseB),
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

func TestQueueCancelsLaterSiblingsAfterFailure(t *testing.T) {
	queue := NewQueue()
	started := make(chan string, 3)
	results := queue.Run(context.Background(), []Task{
		{
			ID: "fail", Name: "fail", Traits: barrierTraits(),
			Run: func(context.Context) (string, error) {
				started <- "fail"
				return "", errors.New("boom")
			},
		},
		{
			ID: "after", Name: "after", Traits: concurrentTraits(),
			Run: func(context.Context) (string, error) {
				started <- "after"
				return "should-not-run", nil
			},
		},
	})
	if got := waitStarted(t, started); got != "fail" {
		t.Fatalf("started = %s, want fail", got)
	}
	select {
	case got := <-started:
		t.Fatalf("unexpected start %s after sibling failure", got)
	case <-time.After(50 * time.Millisecond):
	}
	if results[0].Err == nil || results[0].Err.Error() != "boom" {
		t.Fatalf("primary err = %v", results[0].Err)
	}
	if !errors.Is(results[1].Err, ErrSiblingCanceled) {
		t.Fatalf("second err = %v, want ErrSiblingCanceled", results[1].Err)
	}
}

func TestQueueCancelsInFlightConcurrentSiblingsOnFailure(t *testing.T) {
	queue := NewQueue()
	started := make(chan string, 2)
	releaseFail := make(chan struct{})
	resultsCh := make(chan []Result, 1)
	go func() {
		resultsCh <- queue.Run(context.Background(), []Task{
			{
				ID: "ok", Name: "ok", Traits: concurrentTraits(),
				Run: func(ctx context.Context) (string, error) {
					started <- "ok"
					select {
					case <-ctx.Done():
						return "", ctx.Err()
					case <-time.After(time.Second):
						return "late", nil
					}
				},
			},
			{
				ID: "fail", Name: "fail", Traits: concurrentTraits(),
				Run: func(context.Context) (string, error) {
					started <- "fail"
					<-releaseFail
					return "", errors.New("boom")
				},
			},
		})
	}()

	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case id := <-started:
			got[id] = true
		case <-time.After(time.Second):
			t.Fatalf("started = %v, want both", got)
		}
	}
	close(releaseFail)
	results := <-resultsCh

	var primary, sibling int
	for _, result := range results {
		if result.Err != nil && result.Err.Error() == "boom" {
			primary++
			continue
		}
		if errors.Is(result.Err, ErrSiblingCanceled) {
			sibling++
		}
	}
	if primary != 1 || sibling != 1 {
		t.Fatalf("results=%+v primary=%d sibling=%d", results, primary, sibling)
	}
}

func blockingTask(id string, traits tool.ToolTraits, started chan<- string, release <-chan struct{}) Task {
	return Task{ID: id, Name: id, Traits: traits, Run: func(context.Context) (string, error) {
		started <- id
		<-release
		return id, nil
	}}
}

func concurrentTraits() tool.ToolTraits {
	return tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: true, ConcurrencySafe: true}
}

func barrierTraits() tool.ToolTraits {
	return tool.ToolTraits{Exposure: tool.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false}
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
