package transport

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
)

// Factory 按配置构造本机 stdio / streamable-http Transport。
type Factory struct {
	Credentials contract.CredentialResolver
	HTTPClient  *http.Client
}

// NewFactory 创建默认 TransportFactory。
func NewFactory(creds contract.CredentialResolver, httpClient *http.Client) *Factory {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 0} // 单次请求超时由 MCP 会话/ctx 控制
	}
	return &Factory{Credentials: creds, HTTPClient: httpClient}
}

// Build 按 config.Type 构造 Transport。
func (f *Factory) Build(ctx context.Context, cfg model.McpServerConfig) (contract.Transport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch cfg.Type {
	case model.McpTransportStdio, "":
		if strings.TrimSpace(cfg.Command) == "" {
			return nil, fmt.Errorf("mcp server %q: stdio 缺少 command", cfg.Name)
		}
		return &stdioTransport{cfg: cfg}, nil
	case model.McpTransportStreamableHTTP:
		if strings.TrimSpace(cfg.URL) == "" {
			return nil, fmt.Errorf("mcp server %q: streamable_http 缺少 url", cfg.Name)
		}
		headers, err := f.resolveHeaders(ctx, cfg)
		if err != nil {
			return nil, err
		}
		client := f.HTTPClient
		if len(headers) > 0 {
			client = &http.Client{
				Timeout:   f.HTTPClient.Timeout,
				Transport: &headerRoundTripper{base: f.HTTPClient.Transport, headers: headers},
			}
		}
		return &streamHTTPTransport{cfg: cfg, client: client}, nil
	default:
		return nil, fmt.Errorf("mcp server %q: 不支持的 transport type %q", cfg.Name, cfg.Type)
	}
}

func (f *Factory) resolveHeaders(ctx context.Context, cfg model.McpServerConfig) (map[string]string, error) {
	out := make(map[string]string, len(cfg.Headers)+len(cfg.EnvHeaders)+1)
	for k, v := range cfg.Headers {
		out[k] = v
	}
	if f.Credentials != nil {
		envHeaders, err := f.Credentials.ResolveEnvHeaders(ctx, cfg)
		if err != nil {
			return nil, err
		}
		for k, v := range envHeaders {
			out[k] = v
		}
		token, err := f.Credentials.ResolveBearerToken(ctx, cfg)
		if err != nil {
			return nil, err
		}
		if token != "" {
			out["Authorization"] = "Bearer " + token
		}
	} else {
		for k, envName := range cfg.EnvHeaders {
			val := strings.TrimSpace(os.Getenv(envName))
			if val == "" {
				return nil, fmt.Errorf("mcp server %q: env header %s 引用的环境变量 %s 为空", cfg.Name, k, envName)
			}
			out[k] = val
		}
		if env := strings.TrimSpace(cfg.BearerTokenEnv); env != "" {
			token := strings.TrimSpace(os.Getenv(env))
			if token == "" {
				return nil, fmt.Errorf("mcp server %q: bearer_token_env %s 为空或不存在", cfg.Name, env)
			}
			out["Authorization"] = "Bearer " + token
		}
	}
	return out, nil
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := h.base
	if base == nil {
		base = http.DefaultTransport
	}
	clone := req.Clone(req.Context())
	for k, v := range h.headers {
		clone.Header.Set(k, v)
	}
	return base.RoundTrip(clone)
}

// Ensure factory implements contract.
var _ contract.TransportFactory = (*Factory)(nil)
