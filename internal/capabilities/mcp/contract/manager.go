package contract

import (
	"context"
	"encoding/json"

	"genesis-agent/internal/capabilities/mcp/model"
)

// Session 是与单个已连接 MCP server 的会话。
type Session interface {
	Name() string
	ListTools(ctx context.Context) ([]model.ToolSnapshot, error)
	CallTool(ctx context.Context, tool string, args json.RawMessage) (model.ToolResult, error)
	ListResources(ctx context.Context) ([]model.ResourceSnapshot, error)
	ReadResource(ctx context.Context, uri string) (string, error)
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
}

// StateListener 订阅 Manager 生命周期事件。
type StateListener interface {
	OnMCPEvent(ctx context.Context, event model.LifecycleEvent)
}

// Manager 统一编排多个 MCP server 连接的生命周期。
type Manager interface {
	// Sync 按最新 catalog 定义批量连接/断开/重连（阻塞直到本轮连接结束）。
	Sync(ctx context.Context, defs []model.McpServerDefinition) ([]model.ServerState, error)
	// SyncAsync 先应用 catalog（立即返回），再后台批量连接；不阻塞启动路径。
	SyncAsync(ctx context.Context, defs []model.McpServerDefinition) ([]model.ServerState, error)
	// EnsureConnected 确保指定 server 已连接（用于工具首调懒连接/等待后台连接）。
	EnsureConnected(ctx context.Context, server string) error
	// WaitRequired 等待所有 Required server 进入终态（ready/failed/disabled）。
	WaitRequired(ctx context.Context) error
	SessionFor(server string) (Session, bool)
	States() []model.ServerState
	Subscribe(listener StateListener)
	Close(ctx context.Context) error
}
