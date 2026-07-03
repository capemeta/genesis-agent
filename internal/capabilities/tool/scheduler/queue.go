package scheduler

import (
	"context"
	"fmt"
	"sync"

	tool "genesis-agent/internal/capabilities/tool/contract"
)

// Task 是同一轮 LLM 返回的一个 sibling tool call。
type Task struct {
	ID     string
	Name   string
	Params string
	Traits tool.ToolTraits
	Run    func(context.Context) (string, error)
}

// Result 是 Task 的执行结果，顺序与输入任务一致。
type Result struct {
	ID     string
	Name   string
	Output string
	Err    error
}

const defaultMaxConcurrentTasks = 4

// QueueOptions 控制 sibling tool 调度器行为。
type QueueOptions struct {
	MaxConcurrency int
}

// Queue 按 ToolTraits 执行 sibling tool calls。
type Queue struct {
	maxConcurrency int
}

// NewQueue 创建 sibling tool 调度器。
func NewQueue(options ...QueueOptions) *Queue {
	maxConcurrency := defaultMaxConcurrentTasks
	if len(options) > 0 && options[0].MaxConcurrency > 0 {
		maxConcurrency = options[0].MaxConcurrency
	}
	return &Queue{maxConcurrency: maxConcurrency}
}

// Run 执行任务。只读且并发安全的连续任务并行执行；其他任务作为 barrier 串行执行。
func (q *Queue) Run(ctx context.Context, tasks []Task) []Result {
	results := make([]Result, len(tasks))
	for i := range results {
		results[i] = Result{ID: tasks[i].ID, Name: tasks[i].Name}
	}

	for i := 0; i < len(tasks); {
		if isSiblingConcurrent(tasks[i]) {
			j := i + 1
			for j < len(tasks) && isSiblingConcurrent(tasks[j]) {
				j++
			}
			runConcurrent(ctx, tasks[i:j], results[i:j], q.maxConcurrency)
			i = j
			continue
		}
		results[i].Output, results[i].Err = runOne(ctx, tasks[i])
		i++
	}
	return results
}

func isSiblingConcurrent(task Task) bool {
	return task.Traits.ReadOnly && task.Traits.ConcurrencySafe
}

func runConcurrent(ctx context.Context, tasks []Task, results []Result, maxConcurrency int) {
	if maxConcurrency <= 0 {
		maxConcurrency = defaultMaxConcurrentTasks
	}
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for i := range tasks {
		i := i
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i].Err = ctx.Err()
				return
			}
			results[i].Output, results[i].Err = runOne(ctx, tasks[i])
		}()
	}
	wg.Wait()
}

func runOne(ctx context.Context, task Task) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if task.Run == nil {
		return "", fmt.Errorf("tool task run function not configured")
	}
	return task.Run(ctx)
}
