// Package visionio 提供视觉读图并发配额（与设计 §4.3 对齐，默认 cap=3）。
package visionio

import (
	"context"
	"sync"
)

const DefaultMaxConcurrentReads = 3

var (
	mu       sync.Mutex
	readSem  chan struct{}
	semSize  = DefaultMaxConcurrentReads
)

func init() {
	readSem = make(chan struct{}, DefaultMaxConcurrentReads)
}

// SetMaxConcurrent 仅供测试调整配额；生产保持默认 3。
func SetMaxConcurrent(n int) {
	if n <= 0 {
		n = DefaultMaxConcurrentReads
	}
	mu.Lock()
	defer mu.Unlock()
	semSize = n
	readSem = make(chan struct{}, n)
}

// Acquire 获取读图槽位；ctx 取消时返回错误。
func Acquire(ctx context.Context) error {
	mu.Lock()
	sem := readSem
	mu.Unlock()
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release 归还读图槽位。
func Release() {
	mu.Lock()
	sem := readSem
	mu.Unlock()
	select {
	case <-sem:
	default:
	}
}
