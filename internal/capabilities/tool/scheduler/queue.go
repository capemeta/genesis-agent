package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"

	tool "genesis-agent/internal/capabilities/tool/contract"
)

// ErrSiblingCanceled 表示因同轮其他 sibling tool 失败而被取消，尚未执行或未完成。
// 对齐 Kode ToolUseQueue 的 sibling_error；不得当作用户 abort / 整 Run 取消。
var ErrSiblingCanceled = errors.New("sibling tool call errored")

// SiblingErrorContent 是回传模型的 sibling 取消文案（对齐 Kode）。
const SiblingErrorContent = "<tool_use_error>Sibling tool call errored</tool_use_error>"

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

// Run 执行任务。
// ConcurrencySafe=true 的连续任务有界并行；false 作为 ordering barrier 串行。
// 任一 sibling 失败后：取消未完成的并发 sibling，后续未开始任务返回 ErrSiblingCanceled。
// 结果顺序与输入一致；全部结束后再交给调用方组装 ToolResult（整批回传 LLM）。
func (q *Queue) Run(ctx context.Context, tasks []Task) []Result {
	results := make([]Result, len(tasks))
	for i := range results {
		results[i] = Result{ID: tasks[i].ID, Name: tasks[i].Name}
	}

	siblingFailed := false
	for i := 0; i < len(tasks); {
		if siblingFailed {
			results[i].Err = ErrSiblingCanceled
			i++
			continue
		}
		if isSiblingConcurrent(tasks[i]) {
			j := i + 1
			for j < len(tasks) && isSiblingConcurrent(tasks[j]) {
				j++
			}
			runConcurrent(ctx, tasks[i:j], results[i:j], q.maxConcurrency)
			if segmentHasPrimaryFailure(results[i:j], ctx) {
				siblingFailed = true
			}
			i = j
			continue
		}
		results[i].Output, results[i].Err = runOne(ctx, tasks[i])
		if isPrimaryFailure(results[i].Err, ctx) {
			siblingFailed = true
		}
		i++
	}
	return results
}

// isSiblingConcurrent 以 ConcurrencySafe 为准（对齐 Kode/Codex）。
// ReadOnly 只影响资源锁模式，不单独否决并行——否则 Task 等「可并行但非只读」工具会被误串行。
func isSiblingConcurrent(task Task) bool {
	return task.Traits.ConcurrencySafe
}

func runConcurrent(ctx context.Context, tasks []Task, results []Result, maxConcurrency int) {
	if maxConcurrency <= 0 {
		maxConcurrency = defaultMaxConcurrentTasks
	}
	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

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
			case <-batchCtx.Done():
				results[i].Err = batchCtx.Err()
				return
			}
			out, err := runOne(batchCtx, tasks[i])
			results[i].Output, results[i].Err = out, err
			if isPrimaryFailure(err, ctx) {
				cancel()
			}
		}()
	}
	wg.Wait()
	remapCanceledToSibling(results, ctx)
}

func segmentHasPrimaryFailure(results []Result, parentCtx context.Context) bool {
	for _, result := range results {
		if isPrimaryFailure(result.Err, parentCtx) {
			return true
		}
	}
	return false
}

func isPrimaryFailure(err error, parentCtx context.Context) bool {
	if err == nil || errors.Is(err, ErrSiblingCanceled) {
		return false
	}
	// 父 context 已取消：属于用户 abort / Run 取消，不是 sibling 主失败。
	if parentCtx.Err() != nil && errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

func remapCanceledToSibling(results []Result, parentCtx context.Context) {
	if parentCtx.Err() != nil {
		return
	}
	hasPrimary := false
	for _, result := range results {
		if isPrimaryFailure(result.Err, parentCtx) {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary {
		return
	}
	for i := range results {
		if errors.Is(results[i].Err, context.Canceled) {
			results[i].Err = ErrSiblingCanceled
			results[i].Output = ""
		}
	}
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
