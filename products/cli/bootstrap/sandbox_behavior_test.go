package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	approvalmodel "genesis-agent/internal/capabilities/approval/model"
	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger"
	clisandbox "genesis-agent/products/cli/internal/sandbox"
	windowssandbox "genesis-agent/shared/local/sandbox/windows"
)

func TestRunCommandDefaultSandboxDisabled(t *testing.T) {
	t.Run("default profile runs locally", func(t *testing.T) {
		tool := mustRunCommandTool(t, clisandbox.DefaultConfig())
		result := executeRunCommand(t, tool)
		if result.Environment != execmodel.EnvironmentLocal {
			t.Fatalf("Environment = %s, want %s", result.Environment, execmodel.EnvironmentLocal)
		}
		if result.SandboxProvider != "" {
			t.Fatalf("SandboxProvider = %q, want empty", result.SandboxProvider)
		}
		assertCommandSucceeded(t, result)
	})
}

func TestRunCommandOptionalSandboxUsesPlatformProfile(t *testing.T) {
	cfg, err := clisandbox.ParseFlag("optional")
	if err != nil {
		t.Fatal(err)
	}
	tool := mustRunCommandTool(t, cfg)
	result := executeRunCommand(t, tool)
	assertCommandSucceeded(t, result)
	if result.Environment != execmodel.EnvironmentLocal && result.Environment != execmodel.EnvironmentSandbox {
		t.Fatalf("Environment = %s", result.Environment)
	}
	if result.Environment == execmodel.EnvironmentSandbox && result.SandboxProvider != clisandbox.ProviderLocalPlatform {
		t.Fatalf("SandboxProvider = %q, want %q", result.SandboxProvider, clisandbox.ProviderLocalPlatform)
	}
	if result.Environment == execmodel.EnvironmentLocal && len(result.Warnings) == 0 {
		t.Fatalf("local optional fallback should include warning: %+v", result)
	}
}

func TestRunCommandRemoteSandboxUsesGenesisSandboxHTTP(t *testing.T) {
	var gotLease map[string]any
	var gotJob map[string]any
	released := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		switch {
		case r.URL.Path == "/v1/sandboxes:lease" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&gotLease); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sandbox_id":      "sandbox-test",
				"lease_id":        "lease-test",
				"runtime_profile": "code-polyglot-basic",
				"status":          "leased",
			})
		case r.URL.Path == "/v1/jobs" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&gotJob); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"job_id":     "job-test",
				"sandbox_id": "sandbox-test",
				"status":     "succeeded",
			})
		case r.URL.Path == "/v1/jobs/job-test" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"job_id":      "job-test",
				"sandbox_id":  "sandbox-test",
				"status":      "succeeded",
				"exit_code":   0,
				"stdout":      "hello\r\n",
				"stderr":      "",
				"duration_ms": 7,
			})
		case r.URL.Path == "/v1/sandboxes/sandbox-test/release" && r.Method == http.MethodPost:
			released = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := clisandbox.Config{
		Mode:                  clisandbox.ModeRemoteSandbox,
		Execution:             execmodel.SandboxRequired,
		Endpoint:              server.URL,
		APIKey:                "test-token",
		WorkspaceID:           "ws-test",
		DefaultRuntimeProfile: execmodel.RuntimeProfileCodePolyglotBasic,
	}
	tool := mustRunCommandTool(t, cfg)
	result := executeRunCommand(t, tool)
	assertCommandSucceeded(t, result)
	if result.Environment != execmodel.EnvironmentSandbox {
		t.Fatalf("Environment=%s, want sandbox", result.Environment)
	}
	if result.SandboxProvider != clisandbox.ProviderGenesisSandbox {
		t.Fatalf("SandboxProvider=%q, want %q", result.SandboxProvider, clisandbox.ProviderGenesisSandbox)
	}
	if gotLease["workspace_id"] != "ws-test" || gotLease["runtime_profile"] != "code-polyglot-basic" || gotLease["task_type"] != "shell" || gotLease["operation"] != "run_shell" {
		t.Fatalf("lease=%+v", gotLease)
	}
	if gotJob["sandbox_id"] != "sandbox-test" || gotJob["workspace_id"] != "ws-test" || gotJob["runtime_profile"] != "code-polyglot-basic" {
		t.Fatalf("job=%+v", gotJob)
	}
	if _, exists := gotJob["language"]; exists {
		t.Fatalf("shell language should be omitted for sandbox API compatibility: %+v", gotJob)
	}
	if !released {
		t.Fatal("sandbox was not released")
	}
}

func TestRunCommandRequiredSandboxFailsClosedOnUnsupportedWindowsPolicy(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows process-constrained policy is the current unsupported required-sandbox case")
	}
	cfg, err := clisandbox.ParseFlag("required")
	if err != nil {
		t.Fatal(err)
	}
	tool := mustRunCommandTool(t, cfg)
	_, err = tool.Execute(cliTestRunContext(), runCommandParams)
	if code := execcontract.CodeOf(err); code != execcontract.ErrCodeSandboxPolicyUnsupported {
		t.Fatalf("CodeOf(err) = %s, want %s (%v)", code, execcontract.ErrCodeSandboxPolicyUnsupported, err)
	}
}

func TestFileDiscoveryUsesWorkspaceRootAndSkipsNoiseDirs(t *testing.T) {
	workspace := newTestWorkspace(t, "glob-workspace-*")
	if err := os.WriteFile(filepath.Join(workspace, "ultra5-comparison-summary.md"), []byte("summary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".gocache", "01"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".gocache", "01", "large.bin"), make([]byte, 5*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "node_modules", "pkg", "large.bin"), make([]byte, 5*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	tools, _, err := buildProductTools(clisandbox.DefaultConfig(), allowBootstrapApproval{}, logger.NewNop(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	var globTool toolcontract.Tool
	var walkTool toolcontract.Tool
	for _, candidate := range tools {
		switch candidate.GetInfo().Name {
		case "glob":
			globTool = candidate
		case "walk_dir":
			walkTool = candidate
		}
	}
	if globTool == nil {
		t.Fatal("glob tool not found")
	}
	if walkTool == nil {
		t.Fatal("walk_dir tool not found")
	}
	fileCtx := fileToolContext(workspace)
	out, err := globTool.Execute(fileCtx, `{"pattern":"ultra5-comparison-summary.md"}`)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Matches   []string `json:"matches"`
		Truncated bool     `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal glob result: %v\n%s", err, out)
	}
	if len(result.Matches) != 1 || result.Matches[0] != "ultra5-comparison-summary.md" {
		t.Fatalf("matches=%+v output=%s", result.Matches, out)
	}
	if result.Truncated {
		t.Fatalf("exact glob should not be truncated by noise: %s", out)
	}

	walkOut, err := walkTool.Execute(fileCtx, `{"path":".","max_depth":1}`)
	if err != nil {
		t.Fatal(err)
	}
	var walkResult struct {
		Entries []struct {
			Path string `json:"path"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(walkOut), &walkResult); err != nil {
		t.Fatalf("unmarshal walk result: %v\n%s", err, walkOut)
	}
	for _, entry := range walkResult.Entries {
		if strings.HasPrefix(entry.Path, ".gocache") || strings.HasPrefix(entry.Path, "node_modules") {
			t.Fatalf("walk_dir leaked noise entry %+v output=%s", entry, walkOut)
		}
	}
}

type allowBootstrapApproval struct{}

func (allowBootstrapApproval) Authorize(context.Context, approvalmodel.Request) (approvalmodel.Decision, error) {
	return approvalmodel.Decision{Type: approvalmodel.DecisionApproved, Reason: "test allow"}, nil
}

const runCommandParams = `{"command":"echo hello","timeout_ms":30000}`

type runCommandResult struct {
	Environment     execmodel.ExecutionEnvironment `json:"environment"`
	SandboxProvider string                         `json:"sandbox_provider"`
	Stdout          string                         `json:"stdout"`
	Stderr          string                         `json:"stderr"`
	ExitCode        int                            `json:"exit_code"`
	TimedOut        bool                           `json:"timed_out"`
	Warnings        []string                       `json:"warnings"`
}

func mustRunCommandTool(t *testing.T, cfg clisandbox.Config) toolcontract.Tool {
	t.Helper()
	if runtime.GOOS == "windows" {
		windowssandbox.SetSandboxDirOverride(t.TempDir())
		t.Cleanup(func() {
			windowssandbox.SetSandboxDirOverride("")
		})
	}
	workspace := newTestWorkspace(t, "sandbox-behavior-*")
	if cfg.Execution != execmodel.SandboxDisabled && cfg.Mode == "" {
		cfg.Mode = clisandbox.ModePlatform
	}
	tools, _, err := buildProductTools(cfg, allowBootstrapApproval{}, logger.NewNop(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range tools {
		if candidate.GetInfo().Name == "run_command" {
			return candidate
		}
	}
	t.Fatal("run_command tool not found")
	return nil
}

func newTestWorkspace(t *testing.T, pattern string) string {
	t.Helper()
	parent := filepath.Join(".gotmp", "test-workspaces")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	workspace, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workspace) })
	return workspace
}
func executeRunCommand(t *testing.T, tool toolcontract.Tool) runCommandResult {
	t.Helper()
	out, err := tool.Execute(cliTestRunContext(), runCommandParams)
	if err != nil {
		t.Fatal(err)
	}
	var result runCommandResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v\n%s", err, out)
	}
	return result
}

func cliTestRunContext() context.Context {
	ctx := contextutil.WithRunID(context.Background(), "run-cli-sandbox-test")
	ctx = contextutil.WithSessionID(ctx, "session-cli-sandbox-test")
	binding := execmodel.ExecutionBinding{ID: "binding-cli-sandbox-test", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyPermissionOnly, Owner: execmodel.ExecutionOwnerRef{RunID: "run-cli-sandbox-test", SessionID: "session-cli-sandbox-test"}}
	return workcontract.WithPreparedRun(ctx, workmodel.PreparedRun{Execution: workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: "."}}})
}

func fileToolContext(workspace string) context.Context {
	binding := execmodel.ExecutionBinding{ID: "binding-cli-file-test", Mode: execmodel.WorkspaceModeProject, Access: execmodel.WorkspaceAccessReadWrite, PathPolicy: execmodel.PathPolicyPermissionOnly, Owner: execmodel.ExecutionOwnerRef{RunID: "run-cli-file-test"}}
	return workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Execution: workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: workspace}}})
}

func assertCommandSucceeded(t *testing.T, result runCommandResult) {
	t.Helper()
	if result.TimedOut || result.ExitCode != 0 {
		t.Fatalf("command failed: exit=%d timed_out=%t stdout=%q stderr=%q warnings=%+v", result.ExitCode, result.TimedOut, result.Stdout, result.Stderr, result.Warnings)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Fatalf("Stdout = %q, want contains hello", result.Stdout)
	}
}
