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
	req := BuildApprovalRequest("write_file", OperationWrite, model.ResolvedPath{
		DisplayPath: "system",
		BackendPath: `C:\Windows\System32\config\SAM`,
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
	req := BuildApprovalRequest("write_file", OperationWrite, model.ResolvedPath{
		DisplayPath: ".ssh",
		BackendPath: `C:\Users\dev\.ssh`,
		Scope:       model.PathScopeExternal,
	})
	if req.Metadata["protected"] != "true" || req.Metadata["critical"] != "true" {
		t.Fatalf("metadata = %#v, want protected critical on write", req.Metadata)
	}
}

func TestBuildApprovalRequestIncludesWorkspaceRel(t *testing.T) {
	req := BuildApprovalRequest("read_file", OperationRead, model.ResolvedPath{
		Scope:        model.PathScopeWorkspace,
		WorkspaceRel: "internal/app.go",
		BackendPath:  "D:/workspace/internal/app.go",
	})
	if req.Metadata["workspace_rel"] != "internal/app.go" {
		t.Fatalf("workspace_rel = %q, want internal/app.go", req.Metadata["workspace_rel"])
	}
}

func TestBuildApprovalRequestMultiOSProtectedPaths(t *testing.T) {
	criticalPaths := []string{
		`C:\Windows\System32\config\SAM`,
		`/etc/sudoers`,
		`/Library/Keychains/System.keychain`,
		`/home/user/.aws/credentials`,
		`/home/user/.ssh/id_rsa`,
	}
	for _, path := range criticalPaths {
		req := BuildApprovalRequest("write_file", OperationWrite, model.ResolvedPath{
			DisplayPath: path,
			BackendPath: path,
			Scope:       model.PathScopeExternal,
		})
		if req.Metadata["protected"] != "true" || req.Metadata["critical"] != "true" {
			t.Fatalf("critical path %q metadata = %#v, want protected and critical", path, req.Metadata)
		}
	}

	protectedPaths := []string{
		`C:\Windows\System32\drivers\etc\hosts`,
		`/etc/crontab`,
		`/home/user/.zshrc`,
		`.github/workflows/ci.yml`,
	}
	for _, path := range protectedPaths {
		req := BuildApprovalRequest("write_file", OperationWrite, model.ResolvedPath{
			DisplayPath: path,
			BackendPath: path,
			Scope:       model.PathScopeExternal,
		})
		if req.Metadata["protected"] != "true" {
			t.Fatalf("protected path %q metadata = %#v, want protected=true", path, req.Metadata)
		}
	}
}

