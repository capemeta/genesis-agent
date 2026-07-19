package permission

import (
	"context"
	"path/filepath"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
)

func TestRuntimeFilePermissionsSessionGrantCoversChildren(t *testing.T) {
	perms := NewRuntimeFilePermissions()
	root := filepath.Join(t.TempDir(), "project")
	child := filepath.Join(root, "src", "main.go")
	if err := perms.Remember(context.Background(), fileRequest(approvalmodel.ActionFileRead, root), approvalmodel.Decision{
		Type: approvalmodel.DecisionApprovedForScope, Scope: approvalmodel.GrantScopeSession,
	}); err != nil {
		t.Fatal(err)
	}
	if !perms.IsAllowed(fileRequest(approvalmodel.ActionFileRead, child)) {
		t.Fatal("expected child path to be allowed by parent grant")
	}
	if perms.IsAllowed(fileRequest(approvalmodel.ActionFileWrite, child)) {
		t.Fatal("read grant must not allow write action")
	}
}

func TestRuntimeFilePermissionsMergesParentGrant(t *testing.T) {
	perms := NewRuntimeFilePermissions()
	root := filepath.Join(t.TempDir(), "project")
	child := filepath.Join(root, "src")
	if err := perms.Remember(context.Background(), fileRequest(approvalmodel.ActionFileRead, child), approvalmodel.Decision{
		Type: approvalmodel.DecisionApprovedForScope, Scope: approvalmodel.GrantScopeSession,
	}); err != nil {
		t.Fatal(err)
	}
	if err := perms.Remember(context.Background(), fileRequest(approvalmodel.ActionFileRead, root), approvalmodel.Decision{
		Type: approvalmodel.DecisionApprovedForScope, Scope: approvalmodel.GrantScopeSession,
	}); err != nil {
		t.Fatal(err)
	}
	grants := perms.Grants()
	if len(grants) != 1 || !isWithinPath(child, grants[0].Path) {
		t.Fatalf("grants = %+v, want only parent grant", grants)
	}
}

func TestRuntimeFilePermissionsDirectoryModeGrantsParent(t *testing.T) {
	perms := NewRuntimeFilePermissions()
	root := t.TempDir()
	file := filepath.Join(root, "notes", "a.txt")
	sibling := filepath.Join(root, "notes", "b.txt")
	outside := filepath.Join(root, "other", "c.txt")
	if err := perms.Remember(context.Background(), fileRequest(approvalmodel.ActionFileRead, file), approvalmodel.Decision{
		Type:     approvalmodel.DecisionApprovedForScope,
		Scope:    approvalmodel.GrantScopeSession,
		PathMode: approvalmodel.PathGrantDirectory,
	}); err != nil {
		t.Fatal(err)
	}
	if !perms.IsAllowed(fileRequest(approvalmodel.ActionFileRead, sibling)) {
		t.Fatal("directory mode should cover sibling files")
	}
	if perms.IsAllowed(fileRequest(approvalmodel.ActionFileRead, outside)) {
		t.Fatal("directory mode must not cover outside parent")
	}
}

func TestRuntimeFilePermissionsExactModeDoesNotCoverSibling(t *testing.T) {
	perms := NewRuntimeFilePermissions()
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	sibling := filepath.Join(root, "b.txt")
	if err := perms.Remember(context.Background(), fileRequest(approvalmodel.ActionFileRead, file), approvalmodel.Decision{
		Type:     approvalmodel.DecisionApprovedForScope,
		Scope:    approvalmodel.GrantScopeSession,
		PathMode: approvalmodel.PathGrantExact,
	}); err != nil {
		t.Fatal(err)
	}
	if !perms.IsAllowed(fileRequest(approvalmodel.ActionFileRead, file)) {
		t.Fatal("exact mode should allow same file")
	}
	if perms.IsAllowed(fileRequest(approvalmodel.ActionFileRead, sibling)) {
		t.Fatal("exact mode must not cover sibling")
	}
}

func TestApprovalServiceRemembersScopedFileDecision(t *testing.T) {
	perms := NewRuntimeFilePermissions()
	next := fixedApproval{decision: approvalmodel.Decision{Type: approvalmodel.DecisionApprovedForScope, Scope: approvalmodel.GrantScopeSession}}
	svc := NewApprovalService(next, perms)
	req := fileRequest(approvalmodel.ActionFileRead, filepath.Join(t.TempDir(), "project"))
	if _, err := svc.Authorize(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !perms.IsAllowed(req) {
		t.Fatal("expected scoped decision to be remembered")
	}
}

func TestApprovalServicePersistsProjectGrant(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "grants.yaml")
	store, err := NewFileProjectGrantStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	perms := NewRuntimeFilePermissions()
	perms.SetProjectStore(store)
	file := filepath.Join(dir, "ext", "a.txt")
	next := fixedApproval{decision: approvalmodel.Decision{
		Type:     approvalmodel.DecisionApprovedForScope,
		Scope:    approvalmodel.GrantScopeProject,
		PathMode: approvalmodel.PathGrantDirectory,
	}}
	svc := NewApprovalService(next, perms)
	if _, err := svc.Authorize(context.Background(), fileRequest(approvalmodel.ActionFileRead, file)); err != nil {
		t.Fatal(err)
	}

	reloaded := NewRuntimeFilePermissions()
	reloaded.SetProjectStore(store)
	if err := reloaded.LoadProject(context.Background()); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(dir, "ext", "b.txt")
	if !reloaded.IsAllowed(fileRequest(approvalmodel.ActionFileRead, sibling)) {
		t.Fatal("reloaded project directory grant should cover sibling")
	}
}

type fixedApproval struct{ decision approvalmodel.Decision }

func (f fixedApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return f.decision, nil
}

func fileRequest(action approvalmodel.Action, path string) approvalmodel.Request {
	return approvalmodel.Request{
		Action: action,
		Resource: approvalmodel.Resource{URI: "file://" + filepath.ToSlash(path), Metadata: map[string]string{
			"backend": path,
			"scope":   "external",
		}},
		Metadata: map[string]string{"backend": path, "scope": "external"},
	}
}
