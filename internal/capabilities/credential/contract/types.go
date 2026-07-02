// Package credential 定义密钥管理契约。
package credential

import (
	"context"
	"time"
)

type CredentialType string

const (
	CredentialTypeAPIKey      CredentialType = "api_key"
	CredentialTypeBearerToken CredentialType = "bearer_token"
	CredentialTypeBasicAuth   CredentialType = "basic_auth"
	CredentialTypeCustom      CredentialType = "custom"
)

type CredentialStatus string

const (
	CredentialStatusActive   CredentialStatus = "active"
	CredentialStatusDisabled CredentialStatus = "disabled"
)

type CredentialRef struct {
	TenantID string
	ID       string
}

type CredentialMeta struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id"`
	ProjectID   string            `json:"project_id,omitempty"`
	Name        string            `json:"name"`
	Type        CredentialType    `json:"type"`
	Status      CredentialStatus  `json:"status"`
	Version     int               `json:"version"`
	Description string            `json:"description,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	RotatedAt   *time.Time        `json:"rotated_at,omitempty"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time        `json:"last_used_at,omitempty"`
}

type CreateCredentialRequest struct {
	ID          string            `json:"id,omitempty"`
	TenantID    string            `json:"tenant_id"`
	ProjectID   string            `json:"project_id,omitempty"`
	Name        string            `json:"name"`
	Type        CredentialType    `json:"type"`
	Secret      string            `json:"secret"`
	Description string            `json:"description,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
}

type UpdateCredentialRequest struct {
	Ref         CredentialRef
	Secret      string
	Description string
	Tags        []string
	Metadata    map[string]string
	ExpiresAt   *time.Time
}

type CredentialFilter struct {
	TenantID  string
	ProjectID string
	Status    CredentialStatus
}

type ResolvePurpose struct {
	TenantID   string
	ProjectID  string
	AgentID    string
	ToolName   string
	SkillName  string
	Operation  string
	Connection string
}

type ResolvedCredential struct {
	Meta   CredentialMeta
	Secret string
}

type Store interface {
	Create(ctx context.Context, req CreateCredentialRequest) (*CredentialMeta, error)
	Update(ctx context.Context, req UpdateCredentialRequest) (*CredentialMeta, error)
	GetMeta(ctx context.Context, ref CredentialRef) (*CredentialMeta, error)
	Resolve(ctx context.Context, ref CredentialRef, purpose ResolvePurpose) (*ResolvedCredential, error)
	Delete(ctx context.Context, ref CredentialRef) error
	List(ctx context.Context, filter CredentialFilter) ([]*CredentialMeta, error)
}

type Service interface {
	Create(ctx context.Context, req CreateCredentialRequest) (*CredentialMeta, error)
	Update(ctx context.Context, req UpdateCredentialRequest) (*CredentialMeta, error)
	GetMeta(ctx context.Context, ref CredentialRef) (*CredentialMeta, error)
	Resolve(ctx context.Context, ref CredentialRef, purpose ResolvePurpose) (*ResolvedCredential, error)
	Delete(ctx context.Context, ref CredentialRef) error
	List(ctx context.Context, filter CredentialFilter) ([]*CredentialMeta, error)
}
