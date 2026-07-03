package filesystem

import (
	"context"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	"genesis-agent/internal/platform/config"
)

func TestMatcherWorkspaceWriteAllowed(t *testing.T) {
	matcher := New(defaults(), files())
	result, ok, err := matcher.Match(context.Background(), fileReq(approvalmodel.ActionFileWrite, "workspace", "write", `D:\work\a.txt`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || result.Type != approvalmodel.PolicyAllow {
		t.Fatalf("result=%+v ok=%v, want allow", result, ok)
	}
}

func TestMatcherWorkspaceDeleteAsks(t *testing.T) {
	matcher := New(defaults(), files())
	result, ok, err := matcher.Match(context.Background(), fileReq(approvalmodel.Action("file.delete"), "workspace", "delete", `D:\work\a.txt`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || result.Type != approvalmodel.PolicyAsk || result.Risk != approvalmodel.RiskHigh {
		t.Fatalf("result=%+v ok=%v, want ask high", result, ok)
	}
}

func TestMatcherWorkspaceMetadataWriteDenied(t *testing.T) {
	req := fileReq(approvalmodel.ActionFileWrite, "workspace", "write", `D:\work\.git\config`)
	req.Metadata["workspace_metadata_write"] = "true"
	matcher := New(defaults(), files())
	result, ok, err := matcher.Match(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || result.Type != approvalmodel.PolicyDeny || result.Risk != approvalmodel.RiskCritical {
		t.Fatalf("result=%+v ok=%v, want deny critical", result, ok)
	}
}

func TestMatcherExternalDeleteDenied(t *testing.T) {
	matcher := New(defaults(), files())
	result, ok, err := matcher.Match(context.Background(), fileReq(approvalmodel.Action("file.delete"), "external", "delete", `D:\outside\a.txt`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || result.Type != approvalmodel.PolicyDeny {
		t.Fatalf("result=%+v ok=%v, want deny", result, ok)
	}
}

func TestMatcherDenyPathOverridesAllowPath(t *testing.T) {
	cfg := files()
	cfg.AllowPaths = []config.PolicyPathRuleConfig{{Path: `D:\data`, Operations: []string{"read"}}}
	cfg.DenyPaths = []config.PolicyPathRuleConfig{{Path: `D:\data\secret`, Operations: []string{"read"}}}
	matcher := New(defaults(), cfg)
	result, ok, err := matcher.Match(context.Background(), fileReq(approvalmodel.ActionFileRead, "external", "read", `D:\data\secret\a.txt`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || result.Type != approvalmodel.PolicyDeny {
		t.Fatalf("result=%+v ok=%v, want deny", result, ok)
	}
}

func TestMatcherAllowPathOperationExact(t *testing.T) {
	cfg := files()
	cfg.AllowPaths = []config.PolicyPathRuleConfig{{Path: `D:\data`, Operations: []string{"read"}}}
	matcher := New(defaults(), cfg)
	read, _, err := matcher.Match(context.Background(), fileReq(approvalmodel.ActionFileRead, "external", "read", `D:\data\a.txt`))
	if err != nil {
		t.Fatal(err)
	}
	write, _, err := matcher.Match(context.Background(), fileReq(approvalmodel.ActionFileWrite, "external", "write", `D:\data\a.txt`))
	if err != nil {
		t.Fatal(err)
	}
	if read.Type != approvalmodel.PolicyAllow || write.Type == approvalmodel.PolicyAllow {
		t.Fatalf("read=%+v write=%+v, want read allow and write not allow", read, write)
	}
}

func defaults() config.PolicyDefaultsConfig {
	return config.PolicyDefaultsConfig{Unknown: "ask", AllowedGrantScopes: []string{"once", "session"}}
}

func files() config.PolicyFilesConfig {
	return config.PolicyFilesConfig{
		Default:           "ask",
		Workspace:         config.PolicyFileOperations{Read: "allow", List: "allow", Walk: "allow", Write: "allow", Edit: "allow", Delete: "ask"},
		External:          config.PolicyFileOperations{Read: "ask", List: "ask", Walk: "ask", Write: "ask", Edit: "ask", Delete: "deny"},
		Protected:         config.PolicyDefaultDecision{Default: "deny"},
		WorkspaceMetadata: config.PolicyWorkspaceMetadata{Write: "deny", Paths: []string{".git", ".agents", ".codex"}},
	}
}

func fileReq(action approvalmodel.Action, scope string, op string, path string) approvalmodel.Request {
	metadata := map[string]string{"scope": scope, "path_scope": scope, "operation": op, "backend": path}
	return approvalmodel.Request{Action: action, Resource: approvalmodel.Resource{Type: "file", URI: "file://" + path, Metadata: metadata}, Metadata: metadata, SuggestedScopes: []approvalmodel.GrantScope{approvalmodel.GrantScopeOnce, approvalmodel.GrantScopeSession}}
}
