package permission

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
)

func TestRuntimeFilePermissionsConcurrentProjectPersist(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileProjectGrantStore(filepath.Join(dir, "grants.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	perms := NewRuntimeFilePermissions()
	perms.SetProjectStore(store)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := filepath.Join(dir, "f", string(rune('a'+i))+".txt")
			_ = perms.Remember(context.Background(), fileRequest(approvalmodel.ActionFileRead, path), approvalmodel.Decision{
				Type:     approvalmodel.DecisionApprovedForScope,
				Scope:    approvalmodel.GrantScopeProject,
				PathMode: approvalmodel.PathGrantExact,
			})
		}(i)
	}
	wg.Wait()

	reloaded := NewRuntimeFilePermissions()
	reloaded.SetProjectStore(store)
	if err := reloaded.LoadProject(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(reloaded.Grants()); got != 8 {
		t.Fatalf("grants = %d, want 8 after concurrent project persist", got)
	}
}
