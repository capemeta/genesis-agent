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
	perms.Remember(fileRequest(approvalmodel.ActionFileRead, root), approvalmodel.Decision{Type: approvalmodel.DecisionApprovedForScope, Scope: approvalmodel.GrantScopeSession})
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
	perms.Remember(fileRequest(approvalmodel.ActionFileRead, child), approvalmodel.Decision{Type: approvalmodel.DecisionApprovedForScope, Scope: approvalmodel.GrantScopeSession})
	perms.Remember(fileRequest(approvalmodel.ActionFileRead, root), approvalmodel.Decision{Type: approvalmodel.DecisionApprovedForScope, Scope: approvalmodel.GrantScopeSession})
	grants := perms.Grants()
	if len(grants) != 1 || !isWithinPath(child, grants[0].Path) {
		t.Fatalf("grants = %+v, want only parent grant", grants)
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
