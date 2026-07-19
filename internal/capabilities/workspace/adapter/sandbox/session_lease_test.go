package sandbox

import (
	"context"
	"fmt"
	"testing"
	"time"

	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type stubRenewer struct {
	sessionCalls int
	err          error
	at           time.Time
}

func (s *stubRenewer) RenewSession(context.Context, string) (time.Time, error) {
	s.sessionCalls++
	if s.err != nil {
		return time.Time{}, s.err
	}
	return s.at, nil
}

func TestSessionFileLocatorRejectsSandboxIDMetadata(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	locator := RemoteLocator{
		ID: "loc-bad", Authority: RemoteExecutorAuthority, Scheme: SessionFileScheme,
		Workspace: sandboxcontract.WorkspaceRef{
			ID: "session-1", Provider: "genesis-sandbox",
			Metadata: map[string]string{"session_id": "session-1", "workspace_id": "ws-1", "sandbox_id": "sbx-1"},
		},
		Path: "work/out.pptx", Scope: workmodel.ResourceScope{TenantID: "tenant"},
		Version: "sha256:abc", Size: 1, Availability: workmodel.ResourceAvailabilityLeased, ExpiresAt: &expires,
	}
	if err := locator.validate(); err == nil {
		t.Fatal("expected sandbox_id metadata to be rejected")
	}
}

func TestSessionLeaseKeeperRenewsSessionFile(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileRemoteLocatorStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(-time.Minute)
	locator := RemoteLocator{
		ID: "loc-1", Authority: RemoteExecutorAuthority, Scheme: SessionFileScheme,
		Workspace: sandboxcontract.WorkspaceRef{
			ID: "session-1", Provider: "genesis-sandbox",
			Metadata: map[string]string{"session_id": "session-1", "workspace_id": "ws-1"},
		},
		Path: "work/out.pptx", Scope: workmodel.ResourceScope{TenantID: "tenant"},
		Version: "sha256:abc", Size: 1, Availability: workmodel.ResourceAvailabilityLeased, ExpiresAt: &expires,
	}
	if err := store.Create(ctx, locator); err != nil {
		t.Fatal(err)
	}
	renewer := &stubRenewer{at: time.Now().Add(time.Hour)}
	keeper, err := NewSessionLeaseKeeper(store, renewer)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := workmodel.ProducedResourceDescriptor{
		Availability: workmodel.ResourceAvailabilityLeased,
		ExpiresAt:    &expires,
		Source:       workmodel.ResourceRef{Authority: RemoteExecutorAuthority, Scheme: SessionFileScheme, ID: "loc-1", Version: "sha256:abc", Scope: workmodel.ResourceScope{TenantID: "tenant"}},
	}
	if err := keeper.EnsureLeasedReadable(ctx, descriptor); err != nil {
		t.Fatal(err)
	}
	if renewer.sessionCalls != 1 {
		t.Fatalf("sessionCalls=%d", renewer.sessionCalls)
	}
}

func TestSessionLeaseKeeperFailsWhenRenewFails(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileRemoteLocatorStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(-time.Minute)
	locator := RemoteLocator{
		ID: "loc-2", Authority: RemoteExecutorAuthority, Scheme: SessionFileScheme,
		Workspace: sandboxcontract.WorkspaceRef{
			ID: "session-2", Provider: "genesis-sandbox",
			Metadata: map[string]string{"session_id": "session-2", "workspace_id": "ws-1"},
		},
		Path: "work/out.pptx", Scope: workmodel.ResourceScope{TenantID: "tenant"},
		Version: "sha256:abc", Size: 1, Availability: workmodel.ResourceAvailabilityLeased, ExpiresAt: &expires,
	}
	if err := store.Create(ctx, locator); err != nil {
		t.Fatal(err)
	}
	keeper, err := NewSessionLeaseKeeper(store, &stubRenewer{err: fmt.Errorf("boom")})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := workmodel.ProducedResourceDescriptor{
		Availability: workmodel.ResourceAvailabilityLeased,
		ExpiresAt:    &expires,
		Source:       workmodel.ResourceRef{Authority: RemoteExecutorAuthority, Scheme: SessionFileScheme, ID: "loc-2", Version: "sha256:abc", Scope: workmodel.ResourceScope{TenantID: "tenant"}},
	}
	if err := keeper.EnsureLeasedReadable(ctx, descriptor); err == nil {
		t.Fatal("expected error")
	}
}
