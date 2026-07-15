package contract

import (
	"context"

	"genesis-agent/internal/capabilities/mcp/model"
)

// ConnectOptions 控制 Dial 时的客户端行为（不暴露官方 SDK 类型）。
type ConnectOptions struct {
	// OnToolsChanged 在收到 tools/list_changed 通知时回调（可空）。
	OnToolsChanged func()
}

// Transport 是与单个 MCP server 通信的底层连接抽象。
// 实现封装官方 modelcontextprotocol/go-sdk，屏蔽 stdio / streamable-http 差异。
type Transport interface {
	Kind() model.McpTransportType
	// Dial 建立底层会话连接（返回的 session 由 Manager 持有并关闭）。
	Dial(ctx context.Context, opts ConnectOptions) (DialedSession, error)
}

// DialedSession 是 Transport.Dial 产出的原始会话句柄。
type DialedSession interface {
	Close() error
	Underlying() any
}

// TransportFactory 按配置构造 Transport（进程放置策略可在此扩展：本机 / sandbox）。
type TransportFactory interface {
	Build(ctx context.Context, cfg model.McpServerConfig) (Transport, error)
}

// CredentialResolver 解析 streamable-http 的 bearer token / env headers。
type CredentialResolver interface {
	ResolveBearerToken(ctx context.Context, cfg model.McpServerConfig) (string, error)
	ResolveEnvHeaders(ctx context.Context, cfg model.McpServerConfig) (map[string]string, error)
}
