package controller

import (
	"context"
	"fmt"
	"sync"

	"genesis-agent/internal/runtime/multiagent/contract"
)

// MemorySlotLimiter 是 CLI/Desktop 以及当前无持久化 Enterprise 的进程内实现。
type MemorySlotLimiter struct {
	mu        sync.Mutex
	max       int
	reserved  map[contract.SlotToken]string
	committed map[contract.SlotToken]string
	nextToken uint64
}

// NewMemorySlotLimiter 创建会话级内存并发槽；max 必须大于零。
func NewMemorySlotLimiter(max int) (*MemorySlotLimiter, error) {
	if max <= 0 {
		return nil, fmt.Errorf("subagent max concurrent 必须大于 0")
	}
	return &MemorySlotLimiter{max: max, reserved: make(map[contract.SlotToken]string), committed: make(map[contract.SlotToken]string)}, nil
}

func (l *MemorySlotLimiter) Reserve(ctx context.Context, sessionID string, _ int) (contract.SlotToken, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	used := 0
	for _, session := range l.reserved {
		if session == sessionID {
			used++
		}
	}
	for _, session := range l.committed {
		if session == sessionID {
			used++
		}
	}
	if used >= l.max {
		return "", fmt.Errorf("agent concurrent limit reached: max=%d；请减少同批并行 Task 数或改为串行", l.max)
	}
	l.nextToken++
	token := contract.SlotToken(fmt.Sprintf("slot-%d", l.nextToken))
	l.reserved[token] = sessionID
	return token, nil
}

func (l *MemorySlotLimiter) Commit(token contract.SlotToken, agentID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	session, ok := l.reserved[token]
	if !ok {
		return fmt.Errorf("subagent slot token 不存在或已提交")
	}
	delete(l.reserved, token)
	l.committed[token] = session
	return nil
}

func (l *MemorySlotLimiter) Release(token contract.SlotToken) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.reserved, token)
	delete(l.committed, token)
	return nil
}
