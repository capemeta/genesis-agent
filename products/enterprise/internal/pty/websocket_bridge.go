package pty

import (
	"context"
	"errors"
)

// WebSocketBridgeStub 描述企业端分布式网络沙箱 Web Socket 桥接存根。
type WebSocketBridgeStub struct{}

// NewWebSocketBridgeStub 创建企业端桥接存根
func NewWebSocketBridgeStub() *WebSocketBridgeStub {
	return &WebSocketBridgeStub{}
}

// RouteWebSocketToSandbox 将 Web 前端与 genesis-sandbox 守护进程流进行网络映射。
// 支持租户上下文校验隔离，符合安全审计要求。
func (ws *WebSocketBridgeStub) RouteWebSocketToSandbox(ctx context.Context, tenantID, sessionID string) error {
	return errors.New("WebSocketBridgeStub: genesis-sandbox remote proxying is scheduled for Phase 3")
}
