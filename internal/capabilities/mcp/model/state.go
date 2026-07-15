package model

import "time"

// ServerStatus 描述单个 MCP server 连接状态（对齐 Kode WrappedClient 三态 + starting/disabled）。
type ServerStatus string

const (
	ServerStatusStarting  ServerStatus = "starting"
	ServerStatusReady     ServerStatus = "ready"
	ServerStatusFailed    ServerStatus = "failed"
	ServerStatusNeedsAuth ServerStatus = "needs_auth"
	ServerStatusDisabled  ServerStatus = "disabled"
	ServerStatusCancelled ServerStatus = "cancelled"
)

// ServerState 是 Manager 对外暴露的 server 状态快照。
type ServerState struct {
	Name           string
	Status         ServerStatus
	Origin         DefinitionOrigin
	Required       bool
	Error          string
	ToolCount      int
	Tools          []ToolSnapshot
	ConfigKey      string
	LastConnected  time.Time
	LastHealthPing time.Time
	Fatal          bool // Required=true 且连接失败
}

// EventKind MCP 域生命周期事件类型。
type EventKind string

const (
	EventServerStarting EventKind = "mcp.server.starting"
	EventServerReady    EventKind = "mcp.server.ready"
	EventServerFailed   EventKind = "mcp.server.failed"
	EventServerClosed   EventKind = "mcp.server.closed"
	EventToolsChanged   EventKind = "mcp.tools.changed"
)

// LifecycleEvent 供 gateway / Trace / Audit 订阅。
type LifecycleEvent struct {
	Kind   EventKind
	Server string
	State  ServerState
	At     time.Time
}
