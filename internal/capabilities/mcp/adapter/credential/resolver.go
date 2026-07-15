package credential

import (
	"context"
	"fmt"
	"os"
	"strings"

	credentialcontract "genesis-agent/internal/capabilities/credential/contract"
	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
)

// Resolver 从 env / credential 域解析 MCP 远程凭证。
type Resolver struct {
	Credentials credentialcontract.Service
	TenantID    string
}

// New 创建凭证解析器。
func New(creds credentialcontract.Service, tenantID string) *Resolver {
	return &Resolver{Credentials: creds, TenantID: tenantID}
}

func (r *Resolver) ResolveBearerToken(ctx context.Context, cfg model.McpServerConfig) (string, error) {
	if env := strings.TrimSpace(cfg.BearerTokenEnv); env != "" {
		token := strings.TrimSpace(os.Getenv(env))
		if token == "" {
			return "", fmt.Errorf("mcp server %q: bearer_token_env %s 为空或不存在", cfg.Name, env)
		}
		return token, nil
	}
	ref := strings.TrimSpace(cfg.CredentialRef)
	if ref == "" {
		return "", nil
	}
	if r == nil || r.Credentials == nil {
		return "", fmt.Errorf("mcp server %q: credential_ref=%s 但 credential 服务不可用", cfg.Name, ref)
	}
	tenant := strings.TrimSpace(r.TenantID)
	if tenant == "" {
		tenant = "dev"
	}
	resolved, err := r.Credentials.Resolve(ctx, credentialcontract.CredentialRef{TenantID: tenant, ID: ref}, credentialcontract.ResolvePurpose{
		TenantID:  tenant,
		ToolName:  "mcp",
		Operation: "connect",
	})
	if err != nil {
		return "", fmt.Errorf("mcp server %q: 解析 credential_ref 失败: %w", cfg.Name, err)
	}
	if resolved == nil || strings.TrimSpace(resolved.Secret) == "" {
		return "", fmt.Errorf("mcp server %q: credential_ref %s 密钥为空", cfg.Name, ref)
	}
	return strings.TrimSpace(resolved.Secret), nil
}

func (r *Resolver) ResolveEnvHeaders(ctx context.Context, cfg model.McpServerConfig) (map[string]string, error) {
	_ = ctx
	if len(cfg.EnvHeaders) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(cfg.EnvHeaders))
	for header, envName := range cfg.EnvHeaders {
		val := strings.TrimSpace(os.Getenv(envName))
		if val == "" {
			return nil, fmt.Errorf("mcp server %q: env_headers[%s] 引用的环境变量 %s 为空", cfg.Name, header, envName)
		}
		out[header] = val
	}
	return out, nil
}

var _ contract.CredentialResolver = (*Resolver)(nil)
