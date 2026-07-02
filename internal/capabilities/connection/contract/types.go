// Package connection 定义业务连接管理契约。
package connection

import (
	"context"
	"net/http"
	"time"

	platformhttp "genesis-agent/internal/platform/httpclient"
)

type AuthType string

const (
	AuthTypeNone         AuthType = "none"
	AuthTypeAPIKeyHeader AuthType = "api_key_header"
	AuthTypeAPIKeyQuery  AuthType = "api_key_query"
	AuthTypeBearerToken  AuthType = "bearer_token"
	AuthTypeBasicAuth    AuthType = "basic_auth"
	AuthTypeCustomHeader AuthType = "custom_header"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

type Ref struct {
	TenantID string
	ID       string
}

type AuthConfig struct {
	Type          AuthType `json:"type"`
	CredentialRef string   `json:"credential_ref,omitempty"`
	HeaderName    string   `json:"header_name,omitempty"`
	QueryName     string   `json:"query_name,omitempty"`
	Username      string   `json:"username,omitempty"`
}

type RetryPolicy struct {
	MaxAttempts      int      `json:"max_attempts,omitempty"`
	InitialBackoffMS int      `json:"initial_backoff_ms,omitempty"`
	MaxBackoffMS     int      `json:"max_backoff_ms,omitempty"`
	Multiplier       float64  `json:"multiplier,omitempty"`
	Jitter           bool     `json:"jitter,omitempty"`
	RetryStatusCodes []int    `json:"retry_status_codes,omitempty"`
	RetryMethods     []string `json:"retry_methods,omitempty"`
}

type HTTPConnection struct {
	ID             string            `json:"id"`
	TenantID       string            `json:"tenant_id"`
	ProjectID      string            `json:"project_id,omitempty"`
	Name           string            `json:"name"`
	Environment    string            `json:"environment,omitempty"`
	BaseURL        string            `json:"base_url"`
	DefaultHeaders map[string]string `json:"default_headers,omitempty"`
	Auth           AuthConfig        `json:"auth,omitempty"`
	TimeoutMS      int               `json:"timeout_ms,omitempty"`
	Retry          *RetryPolicy      `json:"retry,omitempty"`
	AllowedTools   []string          `json:"allowed_tools,omitempty"`
	AllowedAgents  []string          `json:"allowed_agents,omitempty"`
	Status         Status            `json:"status"`
	Description    string            `json:"description,omitempty"`
	Tags           []string          `json:"tags,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type CreateHTTPRequestConnectionRequest struct {
	ID             string            `json:"id,omitempty"`
	TenantID       string            `json:"tenant_id"`
	ProjectID      string            `json:"project_id,omitempty"`
	Name           string            `json:"name"`
	Environment    string            `json:"environment,omitempty"`
	BaseURL        string            `json:"base_url"`
	DefaultHeaders map[string]string `json:"default_headers,omitempty"`
	Auth           AuthConfig        `json:"auth,omitempty"`
	TimeoutMS      int               `json:"timeout_ms,omitempty"`
	Retry          *RetryPolicy      `json:"retry,omitempty"`
	AllowedTools   []string          `json:"allowed_tools,omitempty"`
	AllowedAgents  []string          `json:"allowed_agents,omitempty"`
	Description    string            `json:"description,omitempty"`
	Tags           []string          `json:"tags,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type UpdateHTTPRequestConnectionRequest struct {
	Ref            Ref
	Name           string
	Environment    string
	BaseURL        string
	DefaultHeaders map[string]string
	Auth           *AuthConfig
	TimeoutMS      *int
	Retry          *RetryPolicy
	AllowedTools   []string
	AllowedAgents  []string
	Status         Status
	Description    string
	Tags           []string
	Metadata       map[string]string
}

type Filter struct {
	TenantID    string
	ProjectID   string
	Environment string
	Status      Status
}

type HTTPResolveRequest struct {
	TenantID      string
	ProjectID     string
	ConnectionRef string
	AgentID       string
	ToolName      string
	SkillName     string
	Operation     string
}

type ResolvedHTTPConnection struct {
	Connection HTTPConnection
	BaseURL    string
	Headers    http.Header
	Auth       *platformhttp.AuthConfig
	Timeout    time.Duration
	Retry      *platformhttp.RetryPolicy
}

type Store interface {
	CreateHTTP(ctx context.Context, req CreateHTTPRequestConnectionRequest) (*HTTPConnection, error)
	UpdateHTTP(ctx context.Context, req UpdateHTTPRequestConnectionRequest) (*HTTPConnection, error)
	GetHTTP(ctx context.Context, ref Ref) (*HTTPConnection, error)
	DeleteHTTP(ctx context.Context, ref Ref) error
	ListHTTP(ctx context.Context, filter Filter) ([]*HTTPConnection, error)
}

type HTTPResolver interface {
	ResolveForHTTP(ctx context.Context, req HTTPResolveRequest) (*ResolvedHTTPConnection, error)
}

type Service interface {
	Store
	HTTPResolver
}
