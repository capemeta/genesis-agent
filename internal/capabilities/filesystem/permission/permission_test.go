package permission

import (
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/capabilities/filesystem/model"
)

func TestBuildApprovalRequestAllowsWorkspaceRead(t *testing.T) {
	req := BuildApprovalRequest("read_file", OperationRead, model.ResolvedPath{
		DisplayPath:  "a.txt",
		BackendPath:  `D:\workspace\go\genesis-agent\a.txt`,
		WorkspaceID:  "local",
		WorkspaceRel: "a.txt",
		Scope:        model.PathScopeWorkspace,
	})
	if req.Action != approvalmodel.ActionFileRead {
		t.Fatalf("Action = %q, want file.read", req.Action)
	}
	if req.Metadata["scope"] != string(model.PathScopeWorkspace) {
		t.Fatalf("scope metadata = %q, want workspace", req.Metadata["scope"])
	}
}

func TestBuildApprovalRequestMarksProtectedPath(t *testing.T) {
	req := BuildApprovalRequest("read_file", OperationRead, model.ResolvedPath{
		DisplayPath: "system",
		BackendPath: `C:\Windows\System32\config`,
		Scope:       model.PathScopeProtected,
	})
	if req.Metadata["protected"] != "true" || req.Metadata["critical"] != "true" {
		t.Fatalf("metadata = %#v, want protected critical", req.Metadata)
	}
}

func TestBuildApprovalRequestDeniesWorkspaceMetadataWrite(t *testing.T) {
	for _, rel := range []string{".git/config", ".codex/settings.json", ".agents/state.json"} {
		req := BuildApprovalRequest("write_file", OperationWrite, model.ResolvedPath{
			DisplayPath:  rel,
			BackendPath:  `D:\workspace\go\genesis-agent\` + rel,
			WorkspaceID:  "local",
			WorkspaceRel: rel,
			Scope:        model.PathScopeWorkspace,
		})
		if req.Metadata["workspace_metadata_write"] != "true" || req.Metadata["critical"] != "true" {
			t.Fatalf("%s metadata = %#v, want workspace metadata critical", rel, req.Metadata)
		}
	}
}

func TestBuildApprovalRequestMetadataReadIsNotCritical(t *testing.T) {
	req := BuildApprovalRequest("read_file", OperationRead, model.ResolvedPath{
		DisplayPath:  ".git/config",
		BackendPath:  `D:\workspace\go\genesis-agent\.git\config`,
		WorkspaceID:  "local",
		WorkspaceRel: ".git/config",
		Scope:        model.PathScopeWorkspace,
	})
	if req.Metadata["critical"] == "true" || req.Metadata["workspace_metadata_write"] == "true" {
		t.Fatalf("metadata = %#v, want metadata read not critical", req.Metadata)
	}
}

func TestBuildApprovalRequestMarksSSHDirectoryItselfProtected(t *testing.T) {
	req := BuildApprovalRequest("read_file", OperationRead, model.ResolvedPath{
		DisplayPath: ".ssh",
		BackendPath: `C:\Users\dev\.ssh`,
		Scope:       model.PathScopeExternal,
	})
	if req.Metadata["protected"] != "true" || req.Metadata["critical"] != "true" {
		t.Fatalf("metadata = %#v, want protected critical", req.Metadata)
	}
}
