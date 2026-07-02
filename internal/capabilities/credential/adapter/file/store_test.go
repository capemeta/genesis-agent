package file

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	credential "genesis-agent/internal/capabilities/credential/contract"
)

func TestStoreEncryptsAndResolvesCredential(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir(), "local-master-key")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	meta, err := store.Create(context.Background(), credential.CreateCredentialRequest{
		ID:       "crm-token",
		TenantID: "dev",
		Name:     "CRM token",
		Type:     credential.CredentialTypeBearerToken,
		Secret:   "super-secret-token",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if meta.ID != "crm-token" {
		t.Fatalf("unexpected credential id: %s", meta.ID)
	}

	data, err := os.ReadFile(filepath.Join(store.root, "credentials", "tenants", "dev", "crm-token.json"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(data), "super-secret-token") {
		t.Fatal("credential file contains plaintext secret")
	}

	resolved, err := store.Resolve(context.Background(), credential.CredentialRef{TenantID: "dev", ID: "crm-token"}, credential.ResolvePurpose{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Secret != "super-secret-token" {
		t.Fatalf("unexpected resolved secret: %q", resolved.Secret)
	}
	if resolved.Meta.LastUsedAt == nil {
		t.Fatal("LastUsedAt was not updated")
	}
}
