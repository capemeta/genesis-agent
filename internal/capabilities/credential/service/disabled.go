package service

import (
	"context"
	"fmt"

	credential "genesis-agent/internal/capabilities/credential/contract"
)

type disabledStore struct {
	reason string
}

func NewDisabled(reason string) *Service {
	return New(&disabledStore{reason: reason})
}

func (s *disabledStore) Create(context.Context, credential.CreateCredentialRequest) (*credential.CredentialMeta, error) {
	return nil, s.err()
}

func (s *disabledStore) Update(context.Context, credential.UpdateCredentialRequest) (*credential.CredentialMeta, error) {
	return nil, s.err()
}

func (s *disabledStore) GetMeta(context.Context, credential.CredentialRef) (*credential.CredentialMeta, error) {
	return nil, s.err()
}

func (s *disabledStore) Resolve(context.Context, credential.CredentialRef, credential.ResolvePurpose) (*credential.ResolvedCredential, error) {
	return nil, s.err()
}

func (s *disabledStore) Delete(context.Context, credential.CredentialRef) error {
	return s.err()
}

func (s *disabledStore) List(context.Context, credential.CredentialFilter) ([]*credential.CredentialMeta, error) {
	return nil, s.err()
}

func (s *disabledStore) err() error {
	if s.reason == "" {
		return fmt.Errorf("credential store 未启用")
	}
	return fmt.Errorf("credential store 未启用: %s", s.reason)
}
