package app

import (
	"testing"

	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestBindProjectRootScopeBindsUnscopedProductResource(t *testing.T) {
	original := &workmodel.ResourceRef{Authority: "host", Scheme: "project", ID: "project"}
	scope := workmodel.ResourceScope{TenantID: "tenant", ProjectID: "project-id", UserID: "user"}
	bound, err := bindProjectRootScope(original, scope)
	if err != nil {
		t.Fatal(err)
	}
	if bound == original || bound.Scope != scope {
		t.Fatalf("project root was not copied and bound: original=%+v bound=%+v", original, bound)
	}
	if original.Scope != (workmodel.ResourceScope{}) {
		t.Fatalf("product configuration was mutated: %+v", original)
	}
}

func TestBindProjectRootScopeRejectsConflictingAuthorityScope(t *testing.T) {
	ref := &workmodel.ResourceRef{Authority: "tenant", Scheme: "project", ID: "project", Scope: workmodel.ResourceScope{TenantID: "tenant-a", ProjectID: "project-a"}}
	if _, err := bindProjectRootScope(ref, workmodel.ResourceScope{TenantID: "tenant-b", ProjectID: "project-a"}); err == nil {
		t.Fatal("cross-tenant project root must fail")
	}
	if _, err := bindProjectRootScope(ref, workmodel.ResourceScope{TenantID: "tenant-a", ProjectID: "project-b"}); err == nil {
		t.Fatal("cross-project project root must fail")
	}
}
