package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	connection "genesis-agent/internal/capabilities/connection/contract"
	credential "genesis-agent/internal/capabilities/credential/contract"
	platformhttp "genesis-agent/internal/platform/httpclient"
)

type Service struct {
	store      connection.Store
	credential credential.Service
}

func New(store connection.Store, credentialSvc credential.Service) *Service {
	return &Service{store: store, credential: credentialSvc}
}

func (s *Service) CreateHTTP(ctx context.Context, req connection.CreateHTTPRequestConnectionRequest) (*connection.HTTPConnection, error) {
	if strings.TrimSpace(req.TenantID) == "" {
		return nil, fmt.Errorf("tenant_id 不能为空")
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("name 不能为空")
	}
	if strings.TrimSpace(req.BaseURL) == "" {
		return nil, fmt.Errorf("base_url 不能为空")
	}
	if req.Auth.Type == "" {
		req.Auth.Type = connection.AuthTypeNone
	}
	return s.store.CreateHTTP(ctx, req)
}

func (s *Service) UpdateHTTP(ctx context.Context, req connection.UpdateHTTPRequestConnectionRequest) (*connection.HTTPConnection, error) {
	if strings.TrimSpace(req.Ref.TenantID) == "" || strings.TrimSpace(req.Ref.ID) == "" {
		return nil, fmt.Errorf("connection ref 不完整")
	}
	return s.store.UpdateHTTP(ctx, req)
}

func (s *Service) GetHTTP(ctx context.Context, ref connection.Ref) (*connection.HTTPConnection, error) {
	if strings.TrimSpace(ref.TenantID) == "" || strings.TrimSpace(ref.ID) == "" {
		return nil, fmt.Errorf("connection ref 不完整")
	}
	return s.store.GetHTTP(ctx, ref)
}

func (s *Service) DeleteHTTP(ctx context.Context, ref connection.Ref) error {
	if strings.TrimSpace(ref.TenantID) == "" || strings.TrimSpace(ref.ID) == "" {
		return fmt.Errorf("connection ref 不完整")
	}
	return s.store.DeleteHTTP(ctx, ref)
}

func (s *Service) ListHTTP(ctx context.Context, filter connection.Filter) ([]*connection.HTTPConnection, error) {
	return s.store.ListHTTP(ctx, filter)
}

func (s *Service) ResolveForHTTP(ctx context.Context, req connection.HTTPResolveRequest) (*connection.ResolvedHTTPConnection, error) {
	tenantID := strings.TrimSpace(req.TenantID)
	if tenantID == "" {
		tenantID = "dev"
	}
	conn, err := s.store.GetHTTP(ctx, connection.Ref{TenantID: tenantID, ID: strings.TrimSpace(req.ConnectionRef)})
	if err != nil {
		return nil, fmt.Errorf("读取 HTTP connection 失败: %w", err)
	}
	if conn.Status != "" && conn.Status != connection.StatusActive {
		return nil, fmt.Errorf("connection %s 已禁用", conn.ID)
	}

	auth, err := s.resolveAuth(ctx, conn, req)
	if err != nil {
		return nil, err
	}

	return &connection.ResolvedHTTPConnection{
		Connection: *conn,
		BaseURL:    conn.BaseURL,
		Headers:    toHTTPHeader(conn.DefaultHeaders),
		Auth:       auth,
		Timeout:    time.Duration(conn.TimeoutMS) * time.Millisecond,
		Retry:      toHTTPRetry(conn.Retry),
	}, nil
}

func (s *Service) resolveAuth(ctx context.Context, conn *connection.HTTPConnection, req connection.HTTPResolveRequest) (*platformhttp.AuthConfig, error) {
	authType := conn.Auth.Type
	if authType == "" || authType == connection.AuthTypeNone {
		return nil, nil
	}
	if strings.TrimSpace(conn.Auth.CredentialRef) == "" {
		return nil, fmt.Errorf("connection %s 缺少 credential_ref", conn.ID)
	}
	if s.credential == nil {
		return nil, fmt.Errorf("credential service 未初始化")
	}

	resolved, err := s.credential.Resolve(ctx, credential.CredentialRef{
		TenantID: conn.TenantID,
		ID:       conn.Auth.CredentialRef,
	}, credential.ResolvePurpose{
		TenantID:   conn.TenantID,
		ProjectID:  firstNonEmpty(req.ProjectID, conn.ProjectID),
		AgentID:    req.AgentID,
		ToolName:   req.ToolName,
		SkillName:  req.SkillName,
		Operation:  req.Operation,
		Connection: conn.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("解析 credential 失败: %w", err)
	}

	return &platformhttp.AuthConfig{
		Type:       platformhttp.AuthType(authType),
		HeaderName: strings.TrimSpace(conn.Auth.HeaderName),
		QueryName:  strings.TrimSpace(conn.Auth.QueryName),
		Username:   conn.Auth.Username,
		Password:   resolved.Secret,
		Token:      resolved.Secret,
		Value:      resolved.Secret,
	}, nil
}

func toHTTPHeader(headers map[string]string) http.Header {
	if len(headers) == 0 {
		return nil
	}
	out := make(http.Header, len(headers))
	for key, value := range headers {
		out.Set(key, value)
	}
	return out
}

func toHTTPRetry(policy *connection.RetryPolicy) *platformhttp.RetryPolicy {
	if policy == nil {
		return nil
	}
	return &platformhttp.RetryPolicy{
		MaxAttempts:      policy.MaxAttempts,
		InitialBackoff:   time.Duration(policy.InitialBackoffMS) * time.Millisecond,
		MaxBackoff:       time.Duration(policy.MaxBackoffMS) * time.Millisecond,
		Multiplier:       policy.Multiplier,
		Jitter:           policy.Jitter,
		RetryStatusCodes: append([]int(nil), policy.RetryStatusCodes...),
		RetryMethods:     append([]string(nil), policy.RetryMethods...),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
