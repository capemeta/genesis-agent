package transport

import (
	"context"
	"fmt"
	"net/http"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type streamHTTPTransport struct {
	cfg    model.McpServerConfig
	client *http.Client
}

func (t *streamHTTPTransport) Kind() model.McpTransportType { return model.McpTransportStreamableHTTP }

func (t *streamHTTPTransport) Dial(ctx context.Context, opts contract.ConnectOptions) (contract.DialedSession, error) {
	clientOpts := &mcp.ClientOptions{KeepAlive: 0}
	if opts.OnToolsChanged != nil {
		clientOpts.ToolListChangedHandler = func(context.Context, *mcp.ToolListChangedRequest) {
			opts.OnToolsChanged()
		}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "genesis-agent", Version: "1.0.0"}, clientOpts)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   t.cfg.URL,
		HTTPClient: t.client,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("连接 streamable_http mcp server %q 失败: %w", t.cfg.Name, err)
	}
	return &sdkDialed{session: session}, nil
}

var _ contract.Transport = (*streamHTTPTransport)(nil)
