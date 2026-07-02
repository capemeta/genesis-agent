package service

import (
	"context"
	"fmt"
	"strings"

	credential "genesis-agent/internal/capabilities/credential/contract"
)

type Service struct {
	store credential.Store
}

func New(store credential.Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, req credential.CreateCredentialRequest) (*credential.CredentialMeta, error) {
	if strings.TrimSpace(req.TenantID) == "" {
		return nil, fmt.Errorf("tenant_id 不能为空")
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("name 不能为空")
	}
	if strings.TrimSpace(req.Secret) == "" {
		return nil, fmt.Errorf("secret 不能为空")
	}
	if req.Type == "" {
		req.Type = credential.CredentialTypeAPIKey
	}
	return s.store.Create(ctx, req)
}

func (s *Service) Update(ctx context.Context, req credential.UpdateCredentialRequest) (*credential.CredentialMeta, error) {
	if strings.TrimSpace(req.Ref.TenantID) == "" || strings.TrimSpace(req.Ref.ID) == "" {
		return nil, fmt.Errorf("credential ref 不完整")
	}
	return s.store.Update(ctx, req)
}

func (s *Service) GetMeta(ctx context.Context, ref credential.CredentialRef) (*credential.CredentialMeta, error) {
	return s.store.GetMeta(ctx, ref)
}

func (s *Service) Resolve(ctx context.Context, ref credential.CredentialRef, purpose credential.ResolvePurpose) (*credential.ResolvedCredential, error) {
	if strings.TrimSpace(ref.TenantID) == "" || strings.TrimSpace(ref.ID) == "" {
		return nil, fmt.Errorf("credential ref 不完整")
	}
	return s.store.Resolve(ctx, ref, purpose)
}

func (s *Service) Delete(ctx context.Context, ref credential.CredentialRef) error {
	return s.store.Delete(ctx, ref)
}

func (s *Service) List(ctx context.Context, filter credential.CredentialFilter) ([]*credential.CredentialMeta, error) {
	return s.store.List(ctx, filter)
}
