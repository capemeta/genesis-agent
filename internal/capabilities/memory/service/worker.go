package service

import (
	"context"
	"log"

	contract "genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
)

// ExtractJob 异步记忆抽取的队列任务 payload
type ExtractJob struct {
	Ref      contract.SessionRef
	Messages []*domain.Message
}

// MemoryExtractWorker 处理异步两阶段记忆提炼的后台 Worker
type MemoryExtractWorker struct {
	extractor contract.MemoryExtractor
	ltm       contract.LongTermMemory
	jobQueue  chan ExtractJob
}

// NewMemoryExtractWorker 创建异步记忆提炼 Worker 实例
func NewMemoryExtractWorker(extractor contract.MemoryExtractor, ltm contract.LongTermMemory) *MemoryExtractWorker {
	return &MemoryExtractWorker{
		extractor: extractor,
		ltm:       ltm,
		jobQueue:  make(chan ExtractJob, 100), // 100 缓存队列，防止积压
	}
}

// Submit 提交异步记忆提炼任务（非阻塞，队列若满则自动舍弃并打印 log 警告）
func (w *MemoryExtractWorker) Submit(ref contract.SessionRef, msgs []*domain.Message) {
	if len(msgs) == 0 {
		return
	}

	// 深度拷贝消息对象及其主要字段，实现完美的并发内存隔离，防止数据竞争隐患
	copied := make([]*domain.Message, len(msgs))
	for i, m := range msgs {
		if m != nil {
			copied[i] = &domain.Message{
				UUID:             m.UUID,
				Kind:             m.Kind,
				Role:             m.Role,
				Content:          m.Content,
				ReasoningContent: m.ReasoningContent,
				ToolCallID:       m.ToolCallID,
				ToolCalls:        m.ToolCalls,
				Source:           m.Source,
			}
		}
	}

	job := ExtractJob{
		Ref:      ref,
		Messages: copied,
	}

	select {
	case w.jobQueue <- job:
		// 提交成功
	default:
		// 队列已满，优雅降级丢弃，防止卡死对话主流程
		log.Printf("[WARNING] MemoryExtractWorker: job queue is full (size=100), dropping async extraction job for session %s", ref.SessionID)
	}
}

// Start 启动后台 Goroutine 监听队列，持续进行提炼与落库
func (w *MemoryExtractWorker) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case job, ok := <-w.jobQueue:
				if !ok {
					return
				}
				// 执行第一阶段：LLM 异步提炼
				in := contract.ExtractInput{
					SessionRef: job.Ref,
					Messages:   job.Messages,
				}

				extracted, err := w.extractor.Extract(ctx, in)
				if err != nil {
					// 仅作日志记录，不卡死进程
					log.Printf("[ERROR] MemoryExtractWorker: extract memories failed for session %s: %v", job.Ref.SessionID, err)
					continue
				}

				if len(extracted) == 0 {
					continue
				}

				// 执行第二阶段：提炼结果直接入库（文件存储端 Save 具有合并排重逻辑）
				if err := w.ltm.Save(ctx, job.Ref, extracted); err != nil {
					log.Printf("[ERROR] MemoryExtractWorker: save extracted memories failed: %v", err)
				} else {
					log.Printf("[INFO] MemoryExtractWorker: successfully extracted and saved %d new long term memories for session %s", len(extracted), job.Ref.SessionID)
				}
			}
		}
	}()
}
