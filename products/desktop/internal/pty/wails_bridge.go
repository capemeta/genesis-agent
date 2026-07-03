package pty

import (
	"context"
	"errors"
)

// WailsBridgeStub 描述 Desktop 客户端 Wails IPC 总线桥接存根。
type WailsBridgeStub struct{}

// NewWailsBridgeStub 创建桌面端桥接存根
func NewWailsBridgeStub() *WailsBridgeStub {
	return &WailsBridgeStub{}
}

// PublishSessionLogsStub 将 PTY 会话输出流式发布给 Wails 前端组件
func (w *WailsBridgeStub) PublishSessionLogsStub(ctx context.Context, sessionID string) error {
	return errors.New("WailsBridgeStub: desktop frontends are deferred in Phase 1")
}
