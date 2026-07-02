package service

import (
	"context"
	"testing"

	connection "genesis-agent/internal/capabilities/connection/contract"
	credential "genesis-agent/internal/capabilities/credential/contract"
)

func TestResolveForHTTPInjectsCredential(t *testing.T) {
	t.Parallel()

	store := &fakeConnectionStore{
		conn: &connection.HTTPConnection{
			ID:       "crm",
			TenantID: "dev",
			Name:     "CRM",
			BaseURL:  "https://crm.example.com",
			Auth: connection.AuthConfig{
				Type:          connection.AuthTypeBearerToken,
				CredentialRef: "crm-token",
			},
			Status: connection.StatusActive,
		},
	}
	credentials := fakeCredentialService{secret: "secret-token"}
	svc := New(store, credentials)

	resolved, err := svc.ResolveForHTTP(context.Background(), connection.HTTPResolveRequest{
		TenantID:      "dev",
		ConnectionRef: "crm",
		ToolName:      "http_request",
	})
	if err != nil {
		t.Fatalf("ResolveForHTTP() error = %v", err)
	}
	if resolved.BaseURL != "https://crm.example.com" {
		t.Fatalf("unexpected base url: %s", resolved.BaseURL)
	}
	if resolved.Auth == nil || resolved.Auth.Token != "secret-token" {
		t.Fatalf("credential was not injected: %#v", resolved.Auth)
	}
}

type fakeConnectionStore struct {
	conn *connection.HTTPConnection
}

func (s *fakeConnectionStore) CreateHTTP(context.Context, connection.CreateHTTPRequestConnectionRequest) (*connection.HTTPConnection, error) {
	return s.conn, nil
}

func (s *fakeConnectionStore) UpdateHTTP(context.Context, connection.UpdateHTTPRequestConnectionRequest) (*connection.HTTPConnection, error) {
	return s.conn, nil
}

func (s *fakeConnectionStore) GetHTTP(context.Context, connection.Ref) (*connection.HTTPConnection, error) {
	return s.conn, nil
}

func (s *fakeConnectionStore) DeleteHTTP(context.Context, connection.Ref) error {
	return nil
}

func (s *fakeConnectionStore) ListHTTP(context.Context, connection.Filter) ([]*connection.HTTPConnection, error) {
	return []*connection.HTTPConnection{s.conn}, nil
}

type fakeCredentialService struct {
	secret string
}

func (s fakeCredentialService) Create(context.Context, credential.CreateCredentialRequest) (*credential.CredentialMeta, error) {
	return nil, nil
}

func (s fakeCredentialService) Update(context.Context, credential.UpdateCredentialRequest) (*credential.CredentialMeta, error) {
	return nil, nil
}

func (s fakeCredentialService) GetMeta(context.Context, credential.CredentialRef) (*credential.CredentialMeta, error) {
	return nil, nil
}

func (s fakeCredentialService) Resolve(context.Context, credential.CredentialRef, credential.ResolvePurpose) (*credential.ResolvedCredential, error) {
	return &credential.ResolvedCredential{Secret: s.secret}, nil
}

func (s fakeCredentialService) Delete(context.Context, credential.CredentialRef) error {
	return nil
}

func (s fakeCredentialService) List(context.Context, credential.CredentialFilter) ([]*credential.CredentialMeta, error) {
	return nil, nil
}
