package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
)

func TestNewClearsCustomHTTPClientTimeout(t *testing.T) {
	client, err := New(Config{
		BaseURL: "http://127.0.0.1:18010",
		Client:  &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient.Timeout != 0 {
		t.Fatalf("Timeout=%s, want 0", client.httpClient.Timeout)
	}
}

func TestRunCommandUsesLeaseLifecycleAndMapsResult(t *testing.T) {
	var gotLease leaseRequest
	var got execJobRequest
	released := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-1" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		switch {
		case r.URL.Path == "/v1/sandboxes:lease" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&gotLease); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(sandboxLease{
				SandboxID:      "sandbox-1",
				LeaseID:        "lease-1",
				RuntimeProfile: "office-basic",
				Status:         "leased",
				ExpiresAt:      time.Now().Add(time.Minute),
			})
		case r.URL.Path == "/v1/jobs" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(jobResult{
				JobID:     "job-1",
				SandboxID: "sandbox-1",
				Status:    "succeeded",
			})
		case r.URL.Path == "/v1/jobs/job-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(jobResult{
				JobID:       "job-1",
				SandboxID:   "sandbox-1",
				WorkspaceID: "ws-1",
				Status:      "succeeded",
				ExitCode:    0,
				Stdout:      "ok",
				Stderr:      "warn",
				DurationMS:  12,
				OutputArtifacts: []artifactRecord{{
					ArtifactID:  "artifact-1",
					WorkspaceID: "ws-1",
					JobID:       "job-1",
					Name:        "report.docx",
					Size:        12,
					SHA256:      "abc123",
					MIME:        "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
				}},
				StdoutTruncated: true,
			})
		case r.URL.Path == "/v1/sandboxes/sandbox-1/release" && r.Method == http.MethodPost:
			released = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, APIKey: "token-1", RenewInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunCommand(context.Background(), sandboxcontract.CommandRequest{
		Workspace: sandboxcontract.WorkspaceRef{ID: "ws-1"},
		Command:   execmodel.Command{Command: "echo ok", Cwd: "/workspace", Env: map[string]string{"A": "B"}, Shell: execmodel.ShellSh},
		Sandbox: execmodel.SandboxProfile{
			Provider:       "genesis-sandbox",
			RuntimeProfile: execmodel.RuntimeProfileOfficeBasic,
			TaskType:       execmodel.SandboxTaskOffice,
			Operation:      execmodel.SandboxOperationInspect,
			Language:       "python",
			RiskLevel:      execmodel.SandboxRiskLow,
		},
		Options: execcontract.RunOptions{Timeout: 3 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotLease.WorkspaceID != "ws-1" || gotLease.RuntimeProfile != "office-basic" || gotLease.TaskType != "office" || gotLease.Operation != "inspect" {
		t.Fatalf("unexpected lease: %+v", gotLease)
	}
	if got.SandboxID != "sandbox-1" || got.WorkspaceID != "ws-1" || got.RuntimeProfile != "office-basic" || got.TaskType != "office" || got.Operation != "inspect" {
		t.Fatalf("unexpected request: %+v", got)
	}
	if len(got.Command) != 3 || got.Command[0] != "sh" || got.Command[2] != "echo ok" {
		t.Fatalf("command argv=%+v", got.Command)
	}
	if got.ExecTimeoutSeconds != 3 || got.Spec.WorkingDir != "/workspace" || got.Spec.Env["A"] != "B" || got.Spec.Env["OUTPUT_DIR"] != "/workspace/output" {
		t.Fatalf("request options=%+v", got)
	}
	if result.Environment != execmodel.EnvironmentSandbox || result.SandboxProvider != "genesis-sandbox" || result.Stdout != "ok" || !result.OutputTruncated {
		t.Fatalf("result=%+v", result)
	}
	if result.Cwd != "/workspace" {
		t.Fatalf("result cwd=%q", result.Cwd)
	}
	if len(result.OutputObjects) != 1 || result.OutputObjects[0].ID != "artifact-1" || result.OutputObjects[0].Name != "report.docx" {
		t.Fatalf("output objects=%+v", result.OutputObjects)
	}
	if result.OutputObjects[0].MediaType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("media type=%q", result.OutputObjects[0].MediaType)
	}
	if result.OutputObjects[0].Version != "sha256:abc123" {
		t.Fatalf("version=%q", result.OutputObjects[0].Version)
	}
	if !released {
		t.Fatal("sandbox was not released")
	}
}

func TestRunCommandOutputOnlyWarnsWhenNoArtifacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sandboxes:lease" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(sandboxLease{SandboxID: "sandbox-1", LeaseID: "lease-1", Status: "leased"})
		case r.URL.Path == "/v1/jobs" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(jobResult{JobID: "job-1", SandboxID: "sandbox-1", Status: "succeeded"})
		case r.URL.Path == "/v1/jobs/job-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(jobResult{JobID: "job-1", SandboxID: "sandbox-1", Status: "succeeded", ExitCode: 0})
		case r.URL.Path == "/v1/sandboxes/sandbox-1/release" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, RenewInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunCommand(context.Background(), sandboxcontract.CommandRequest{
		Command: execmodel.Command{Command: "python script.py"},
		Options: execcontract.RunOptions{
			OutputDiscoveryPolicy: execmodel.OutputDiscoveryDeclared,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "NO_OUTPUT_ARTIFACTS") {
		t.Fatalf("warnings=%+v", result.Warnings)
	}
}

func TestRunCommandUsesSandboxCwdAsWorkingDir(t *testing.T) {
	var got execJobRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sandboxes:lease" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(sandboxLease{SandboxID: "sandbox-1", LeaseID: "lease-1", Status: "leased"})
		case r.URL.Path == "/v1/jobs" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(jobResult{JobID: "job-1", SandboxID: "sandbox-1", Status: "succeeded"})
		case r.URL.Path == "/v1/jobs/job-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(jobResult{JobID: "job-1", SandboxID: "sandbox-1", Status: "succeeded", ExitCode: 0})
		case r.URL.Path == "/v1/sandboxes/sandbox-1/release" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, RenewInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunCommand(context.Background(), sandboxcontract.CommandRequest{
		Command: execmodel.Command{Command: "node run.js", Cwd: "/workspace/input/skills/office-ppt/scripts", Shell: execmodel.ShellSh},
		Options: execcontract.RunOptions{Workspace: execmodel.ExecutionWorkspace{WorkDir: "/workspace"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Cwd != "/workspace/input/skills/office-ppt/scripts" {
		t.Fatalf("result cwd=%q", result.Cwd)
	}
	if got.Spec.WorkingDir != "/workspace/input/skills/office-ppt/scripts" {
		t.Fatalf("working_dir=%q", got.Spec.WorkingDir)
	}
	if got.Metadata["cwd"] != "/workspace/input/skills/office-ppt/scripts" {
		t.Fatalf("metadata=%+v", got.Metadata)
	}
}
func TestRunCommandUsesSandboxWorkspaceEnvAndInputArtifacts(t *testing.T) {
	var got execJobRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sandboxes:lease" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(sandboxLease{SandboxID: "sandbox-1", LeaseID: "lease-1", Status: "leased"})
		case r.URL.Path == "/v1/jobs" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(jobResult{JobID: "job-1", SandboxID: "sandbox-1", Status: "succeeded"})
		case r.URL.Path == "/v1/jobs/job-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(jobResult{JobID: "job-1", SandboxID: "sandbox-1", Status: "succeeded", ExitCode: 0})
		case r.URL.Path == "/v1/sandboxes/sandbox-1/release" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, RenewInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunCommand(context.Background(), sandboxcontract.CommandRequest{
		Command: execmodel.Command{
			Command: "python script.py",
			Cwd:     `D:\workspace\go\genesis-agent`,
			Env:     map[string]string{"OUTPUT_DIR": "bad", "A": "B"},
			Shell:   execmodel.ShellSh,
		},
		Options: execcontract.RunOptions{
			StagedInputs: []execmodel.StagedInputRef{{ID: "input-1"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.WorkingDir != "/workspace" {
		t.Fatalf("working_dir=%q", got.Spec.WorkingDir)
	}
	if got.Metadata["cwd"] != "" || got.Metadata["cwd_kind"] != "host_or_logical" {
		t.Fatalf("metadata leaked cwd: %+v", got.Metadata)
	}
	if got.Spec.Env["OUTPUT_DIR"] != "/workspace/output" || got.Spec.Env["INPUT_DIR"] != "/workspace/input" || got.Spec.Env["WORK_DIR"] != "/workspace" || got.Spec.Env["A"] != "B" {
		t.Fatalf("env=%+v", got.Spec.Env)
	}
	if len(got.InputArtifactIDs) != 1 || got.InputArtifactIDs[0] != "input-1" {
		t.Fatalf("input ids=%+v", got.InputArtifactIDs)
	}
}

func TestSessionRunUsesNamedSessionExecAndClose(t *testing.T) {
	var created createSessionRequest
	var execReq execSessionRequest
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatal(err)
			}
			// 休眠创建：active_sandbox_id 为空
			_ = json.NewEncoder(w).Encode(sessionRecord{SessionID: "session-1", WorkspaceID: "ws-1", RuntimeProfile: "code-polyglot-basic", StatePolicy: "session", ExpiresAt: time.Now().Add(time.Hour)})
		case r.URL.Path == "/v1/sessions/session-1/exec" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&execReq); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(execSessionResult{ExitCode: 0, Stdout: "ok", Environment: "sandbox", SandboxID: "sandbox-1", SessionID: "session-1", WorkspaceID: "ws-1"})
		case r.URL.Path == "/v1/sessions/session-1" && r.Method == http.MethodDelete:
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, RenewInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.OpenSession(context.Background(), sandboxcontract.SessionOptions{
		Workspace: sandboxcontract.WorkspaceRef{ID: "ws-1"},
		Sandbox:   execmodel.SandboxProfile{RuntimeProfile: execmodel.RuntimeProfileCodePolyglotBasic, TaskType: execmodel.SandboxTaskCode},
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := session.Workspace()
	if workspace.ID != "session-1" || workspace.Metadata["workspace_id"] != "ws-1" {
		t.Fatalf("session workspace=%+v", workspace)
	}
	if workspace.Metadata["sandbox_id"] != "" {
		t.Fatalf("dormant session should not have sandbox_id: %+v", workspace)
	}
	result, err := session.Run(context.Background(), sandboxcontract.CommandRequest{
		Command: execmodel.Command{Command: "python process.py", Shell: execmodel.ShellSh},
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace = session.Workspace()
	if workspace.Metadata["sandbox_id"] != "sandbox-1" {
		t.Fatalf("exec should backfill sandbox_id: %+v", workspace)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if created.WorkspaceID != "ws-1" || created.StatePolicy != "session" || created.RuntimeProfile != "code-polyglot-basic" || created.WorkspaceRetention != "explicit_delete" {
		t.Fatalf("created session=%+v", created)
	}
	if !deleted {
		t.Fatalf("deleted=%t", deleted)
	}
	if len(execReq.Command) != 3 || execReq.Command[0] != "sh" || execReq.Command[1] != "-lc" {
		t.Fatalf("exec req=%+v", execReq)
	}
	script := execReq.Command[2]
	if !strings.Contains(script, "cd '/workspace'") || !strings.Contains(script, "export WORK_DIR='/workspace'") || !strings.Contains(script, "python process.py") {
		t.Fatalf("session script=%q", script)
	}
	if result.Stdout != "ok" || result.Cwd != "/workspace" {
		t.Fatalf("result=%+v", result)
	}
}

func TestDormantSessionWriteExecSuspendAndReuse(t *testing.T) {
	sandboxIDs := make([]string, 0)
	suspended := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(sessionRecord{
				SessionID: "session-1", WorkspaceID: "ws-1", RuntimeProfile: "office-basic",
				StatePolicy: "session", ExpiresAt: time.Now().Add(time.Hour),
			})
		case r.URL.Path == "/v1/sessions/session-1/files" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(workspaceFileInfo{
				Path: "hello.txt", SandboxPath: "/workspace/hello.txt", Environment: "workspace",
				Name: "hello.txt", Kind: "file", Size: 5, SHA256: "abc",
			})
		case r.URL.Path == "/v1/sessions/session-1/exec" && r.Method == http.MethodPost:
			id := fmt.Sprintf("sandbox-%d", len(sandboxIDs)+1)
			sandboxIDs = append(sandboxIDs, id)
			_ = json.NewEncoder(w).Encode(execSessionResult{
				ExitCode: 0, Stdout: "ok", Environment: "sandbox",
				SandboxID: id, SessionID: "session-1", WorkspaceID: "ws-1",
			})
		case r.URL.Path == "/v1/sessions/session-1/suspend" && r.Method == http.MethodPost:
			suspended = true
			_ = json.NewEncoder(w).Encode(sessionRecord{
				SessionID: "session-1", WorkspaceID: "ws-1", ActiveSandboxID: "",
				ExpiresAt: time.Now().Add(time.Hour),
			})
		case r.URL.Path == "/v1/sessions/session-1" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, RenewInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.OpenSession(context.Background(), sandboxcontract.SessionOptions{
		Workspace: sandboxcontract.WorkspaceRef{ID: "ws-1"},
		Sandbox:   execmodel.SandboxProfile{RuntimeProfile: execmodel.RuntimeProfileOfficeBasic},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.WriteFile(context.Background(), sandboxcontract.WriteFileRequest{
		Workspace: session.Workspace(),
		Path:      fsmodel.ResolvedPath{WorkspaceRel: "hello.txt"},
		Content:   []byte("hello"),
		Options:   fscontract.WriteOptions{Overwrite: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), sandboxcontract.CommandRequest{
		Command: execmodel.Command{Command: "echo ok", Shell: execmodel.ShellSh},
	}); err != nil {
		t.Fatal(err)
	}
	if session.Workspace().Metadata["sandbox_id"] != "sandbox-1" {
		t.Fatalf("sandbox after first exec=%q", session.Workspace().Metadata["sandbox_id"])
	}
	suspendable, ok := session.(sandboxcontract.SuspendableSandboxSession)
	if !ok {
		t.Fatal("session must implement SuspendableSandboxSession")
	}
	if err := suspendable.Suspend(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !suspended {
		t.Fatal("suspend not called")
	}
	if session.Workspace().Metadata["sandbox_id"] != "" {
		t.Fatalf("sandbox_id should clear after suspend: %+v", session.Workspace())
	}
	if _, err := session.Run(context.Background(), sandboxcontract.CommandRequest{
		Command: execmodel.Command{Command: "echo again", Shell: execmodel.ShellSh},
	}); err != nil {
		t.Fatal(err)
	}
	if len(sandboxIDs) != 2 || sandboxIDs[1] != "sandbox-2" {
		t.Fatalf("sandboxIDs=%+v", sandboxIDs)
	}
	if session.Workspace().Metadata["sandbox_id"] != "sandbox-2" {
		t.Fatalf("sandbox after second exec=%q", session.Workspace().Metadata["sandbox_id"])
	}
	_ = session.Close(context.Background())
}

func TestWriteFileCreateOnlyUsesIfNoneMatch(t *testing.T) {
	var gotIfNoneMatch string
	var gotIfMatch string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/session-1/files" || r.Method != http.MethodPut {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		gotIfMatch = r.Header.Get("If-Match")
		_ = json.NewEncoder(w).Encode(workspaceFileInfo{Path: "a.txt", Name: "a.txt", Kind: "file", Size: 1, Environment: "workspace"})
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.WriteFile(context.Background(), sandboxcontract.WriteFileRequest{
		Workspace: sandboxcontract.WorkspaceRef{ID: "session-1"},
		Path:      fsmodel.ResolvedPath{WorkspaceRel: "a.txt"},
		Content:   []byte("x"),
		Options:   fscontract.WriteOptions{Overwrite: false},
	}); err != nil {
		t.Fatal(err)
	}
	if gotIfNoneMatch != "*" || gotIfMatch != "" {
		t.Fatalf("If-None-Match=%q If-Match=%q", gotIfNoneMatch, gotIfMatch)
	}
}

func TestWriteFileExpectedHashUsesIfMatchOnly(t *testing.T) {
	var gotIfNoneMatch string
	var gotIfMatch string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		gotIfMatch = r.Header.Get("If-Match")
		_ = json.NewEncoder(w).Encode(workspaceFileInfo{Path: "a.txt", Name: "a.txt", Kind: "file", Size: 1})
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.WriteFile(context.Background(), sandboxcontract.WriteFileRequest{
		Workspace: sandboxcontract.WorkspaceRef{ID: "session-1"},
		Path:      fsmodel.ResolvedPath{WorkspaceRel: "a.txt"},
		Content:   []byte("x"),
		Options:   fscontract.WriteOptions{Overwrite: false, ExpectedHash: "sha256:abc123"},
	}); err != nil {
		t.Fatal(err)
	}
	if gotIfMatch != "abc123" || gotIfNoneMatch != "" {
		t.Fatalf("If-Match=%q If-None-Match=%q", gotIfMatch, gotIfNoneMatch)
	}
}

func TestWriteFileMapsVersionConflictToModifiedExternally(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(errorResponse{Code: "CONFLICT", Message: "version changed"})
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	err = client.WriteFile(context.Background(), sandboxcontract.WriteFileRequest{
		Workspace: sandboxcontract.WorkspaceRef{ID: "session-1"},
		Path:      fsmodel.ResolvedPath{WorkspaceRel: "a.txt"},
		Content:   []byte("x"),
		Options:   fscontract.WriteOptions{Overwrite: true, ExpectedHash: "abc"},
	})
	if fscontract.CodeOf(err) != fscontract.ErrCodeModifiedExternally {
		t.Fatalf("err=%v code=%s", err, fscontract.CodeOf(err))
	}
}

func TestOpenSessionRejectsMissingExpiresAtAndDeletesSession(t *testing.T) {
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(sessionRecord{SessionID: "session-1", WorkspaceID: "ws-1", ActiveSandboxID: "sandbox-1"})
		case r.URL.Path == "/v1/sessions/session-1" && r.Method == http.MethodDelete:
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, RenewInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.OpenSession(context.Background(), sandboxcontract.SessionOptions{}); err == nil {
		t.Fatal("expected missing expires_at to fail")
	}
	if !deleted {
		t.Fatal("invalid session must be deleted")
	}
}

func TestOpenSessionHeartbeatUsesSessionRenewNotSandboxRenew(t *testing.T) {
	var renewPath string
	var extendSeconds int
	expires := time.Now().Add(2 * time.Minute).UTC().Truncate(time.Second)
	renewed := expires.Add(90 * time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(sessionRecord{
				SessionID: "session-1", WorkspaceID: "ws-1", ActiveSandboxID: "sandbox-1",
				RuntimeProfile: "office-basic", StatePolicy: "session", ExpiresAt: expires,
			})
		case r.URL.Path == "/v1/sessions/session-1/renew" && r.Method == http.MethodPost:
			renewPath = r.URL.Path
			var payload map[string]int
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			extendSeconds = payload["extend_seconds"]
			_ = json.NewEncoder(w).Encode(sessionRecord{
				SessionID: "session-1", WorkspaceID: "ws-1", ActiveSandboxID: "sandbox-1",
				ExpiresAt: renewed,
			})
		case strings.HasPrefix(r.URL.Path, "/v1/sandboxes/") && strings.HasSuffix(r.URL.Path, "/renew"):
			t.Fatalf("session heartbeat must not call sandbox renew: %s", r.URL.Path)
		case r.URL.Path == "/v1/sessions/session-1" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, RenewInterval: 20 * time.Millisecond, RenewExtend: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.OpenSession(context.Background(), sandboxcontract.SessionOptions{
		Workspace: sandboxcontract.WorkspaceRef{ID: "ws-1"},
		Sandbox:   execmodel.SandboxProfile{RuntimeProfile: execmodel.RuntimeProfileOfficeBasic},
	})
	if err != nil {
		t.Fatal(err)
	}
	leased, ok := session.(sandboxcontract.LeasedSandboxSession)
	if !ok {
		t.Fatal("OpenSession must return LeasedSandboxSession")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if renewPath != "" && leased.ExpiresAt().Equal(renewed) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if renewPath != "/v1/sessions/session-1/renew" {
		t.Fatalf("renew path=%q", renewPath)
	}
	if extendSeconds != 90 {
		t.Fatalf("extend_seconds=%d", extendSeconds)
	}
	if !leased.ExpiresAt().Equal(renewed) {
		t.Fatalf("expiresAt=%v want=%v", leased.ExpiresAt(), renewed)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSandboxSessionWorkingDirRejectsHostPaths(t *testing.T) {
	got := sandboxSessionWorkingDir(`D:\workspace\go\genesis-agent`, execmodel.ExecutionWorkspace{WorkDir: "/workspace"})
	if got != "/workspace" {
		t.Fatalf("cwd=%q", got)
	}
}

func TestSessionRunWrapsCwdAndEnvForNamedSession(t *testing.T) {
	var execReq execSessionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(sessionRecord{SessionID: "session-1", WorkspaceID: "ws-1", ActiveSandboxID: "sandbox-1", RuntimeProfile: "office-basic", StatePolicy: "session", ExpiresAt: time.Now().Add(time.Hour)})
		case r.URL.Path == "/v1/sessions/session-1/exec" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&execReq); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(execSessionResult{ExitCode: 0})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, RenewInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.OpenSession(context.Background(), sandboxcontract.SessionOptions{
		Workspace: sandboxcontract.WorkspaceRef{ID: "ws-1"},
		Sandbox:   execmodel.SandboxProfile{RuntimeProfile: execmodel.RuntimeProfileOfficeBasic},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Run(context.Background(), sandboxcontract.CommandRequest{
		Command: execmodel.Command{
			Command: "node deck.js",
			Cwd:     "/workspace/scripts",
			Env: map[string]string{
				"NODE_PATH":    "/opt/genesis-sandbox/image/node_modules",
				"BAD-KEY":      "ignored",
				"QUOTED_VALUE": "it's ok",
			},
			Shell: execmodel.ShellAuto,
		},
		Options: execcontract.RunOptions{Workspace: execmodel.ExecutionWorkspace{WorkDir: "/workspace", InputDir: "/workspace", OutputDir: "/workspace", TmpDir: "/workspace/tmp"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Cwd != "/workspace/scripts" {
		t.Fatalf("result cwd=%q", result.Cwd)
	}
	if len(execReq.Command) != 3 || execReq.Command[0] != "sh" || execReq.Command[1] != "-lc" {
		t.Fatalf("exec req=%+v", execReq)
	}
	script := execReq.Command[2]
	for _, want := range []string{
		"cd '/workspace/scripts'",
		"export NODE_PATH='/opt/genesis-sandbox/image/node_modules'",
		`export QUOTED_VALUE='it'"'"'s ok'`,
		"node deck.js",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("session script missing %q: %q", want, script)
		}
	}
	if strings.Contains(script, "BAD-KEY") {
		t.Fatalf("session script leaked invalid env key: %q", script)
	}
}

func TestSessionWorkspaceFileSystemRejectsParentTraversal(t *testing.T) {
	client, err := New(Config{BaseURL: "http://127.0.0.1:18010"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ReadFile(context.Background(), sandboxcontract.FileRequest{
		Workspace: sandboxcontract.WorkspaceRef{ID: "session-1"},
		Path:      fsmodel.ResolvedPath{WorkspaceRel: "../secret.txt"},
	}, fscontract.ReadOptions{})
	if fscontract.CodeOf(err) != fscontract.ErrCodeInvalidPath {
		t.Fatalf("err=%v code=%s", err, fscontract.CodeOf(err))
	}
}
func TestSessionWorkspaceFileSystemUsesSessionScopedEndpoints(t *testing.T) {
	calls := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case r.URL.Path == "/v1/sessions/session-1/files" && r.Method == http.MethodPut:
			if r.URL.Query().Get("path") != "dir/hello.txt" {
				t.Fatalf("write path=%q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(workspaceFileInfo{Path: "dir/hello.txt", Name: "hello.txt", Kind: "file", Size: 5})
		case r.URL.Path == "/v1/sessions/session-1/files" && r.Method == http.MethodGet:
			if r.URL.Query().Get("path") != "dir/hello.txt" {
				t.Fatalf("read path=%q", r.URL.RawQuery)
			}
			w.Header().Set("X-Workspace-Path", "dir/hello.txt")
			_, _ = w.Write([]byte("hello"))
		case r.URL.Path == "/v1/sessions/session-1/files:list" && r.Method == http.MethodGet:
			if r.URL.Query().Get("path") != "dir" || r.URL.Query().Get("recursive") != "false" {
				t.Fatalf("list query=%q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(workspaceListResult{Path: "dir", Entries: []workspaceFileInfo{{Path: "dir/hello.txt", Name: "hello.txt", Kind: "file", Size: 5}}, Limit: 10})
		case r.URL.Path == "/v1/sessions/session-1/files:stat" && r.Method == http.MethodGet:
			if r.URL.Query().Get("path") != "dir/hello.txt" {
				t.Fatalf("stat query=%q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(workspaceFileInfo{Path: "dir/hello.txt", Name: "hello.txt", Kind: "file", Size: 5})
		case r.URL.Path == "/v1/sessions/session-1/dirs" && r.Method == http.MethodPost:
			if r.URL.Query().Get("path") != "dir/sub" || r.URL.Query().Get("parents") != "true" {
				t.Fatalf("mkdir query=%q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(workspaceFileInfo{Path: "dir/sub", Name: "sub", Kind: "dir"})
		case r.URL.Path == "/v1/sessions/session-1/files" && r.Method == http.MethodDelete:
			if r.URL.Query().Get("path") != "dir/hello.txt" || r.URL.Query().Get("recursive") != "true" {
				t.Fatalf("remove query=%q", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	workspace := sandboxcontract.WorkspaceRef{ID: "session-1", Provider: "genesis-sandbox"}
	filePath := fsmodel.ResolvedPath{WorkspaceRel: "/workspace/dir/hello.txt"}
	if err := client.WriteFile(context.Background(), sandboxcontract.WriteFileRequest{Workspace: workspace, Path: filePath, Content: []byte("hello"), Options: fscontract.WriteOptions{Overwrite: true}}); err != nil {
		t.Fatal(err)
	}
	data, err := client.ReadFile(context.Background(), sandboxcontract.FileRequest{Workspace: workspace, Path: filePath}, fscontract.ReadOptions{MaxBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("data=%q", data)
	}
	entries, err := client.ListDir(context.Background(), sandboxcontract.ListDirRequest{Workspace: workspace, Path: fsmodel.ResolvedPath{WorkspaceRel: "dir"}, Options: fscontract.ListOptions{MaxEntries: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "dir/hello.txt" || entries[0].Type != fsmodel.EntryTypeFile {
		t.Fatalf("entries=%+v", entries)
	}
	stat, err := client.Stat(context.Background(), sandboxcontract.FileRequest{Workspace: workspace, Path: filePath})
	if err != nil {
		t.Fatal(err)
	}
	if stat.Size != 5 || stat.Type != fsmodel.EntryTypeFile {
		t.Fatalf("stat=%+v", stat)
	}
	if err := client.MkdirAll(context.Background(), sandboxcontract.MkdirRequest{Workspace: workspace, Path: fsmodel.ResolvedPath{WorkspaceRel: "dir/sub"}, Options: fscontract.MkdirOptions{Parents: true}}); err != nil {
		t.Fatal(err)
	}
	if err := client.Remove(context.Background(), sandboxcontract.RemoveRequest{Workspace: workspace, Path: filePath, Options: fscontract.RemoveOptions{Recursive: true}}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 6 {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestFSErrorFromExecMapsDockerStatMissingFileToNotFound(t *testing.T) {
	err := execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("runner_failed: docker stat file /workspace/deck_gen.js: Error response from daemon: Could not find the file /workspace/deck_gen.js"))
	got := fsErrorFromExec(err, "deck_gen.js")
	if fscontract.CodeOf(got) != fscontract.ErrCodeNotFound {
		t.Fatalf("err=%v code=%s", got, fscontract.CodeOf(got))
	}
}
func TestRunCommandDoesNotDownloadOutputObjects(t *testing.T) {
	downloaded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sandboxes:lease" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(sandboxLease{
				SandboxID:      "sandbox-1",
				LeaseID:        "lease-1",
				RuntimeProfile: "office-basic",
				Status:         "leased",
				ExpiresAt:      time.Now().Add(time.Minute),
			})
		case r.URL.Path == "/v1/jobs" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(jobResult{JobID: "job-1", SandboxID: "sandbox-1", Status: "succeeded"})
		case r.URL.Path == "/v1/jobs/job-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(jobResult{
				JobID:       "job-1",
				SandboxID:   "sandbox-1",
				WorkspaceID: "ws-1",
				Status:      "succeeded",
				ExitCode:    0,
				OutputArtifacts: []artifactRecord{{
					ArtifactID:  "artifact-1",
					WorkspaceID: "ws-1",
					JobID:       "job-1",
					Name:        "../report.docx",
				}},
			})
		case r.URL.Path == "/v1/artifacts/artifact-1" && r.Method == http.MethodGet:
			downloaded = true
			t.Fatal("RunCommand must not download executor output objects")
		case r.URL.Path == "/v1/sandboxes/sandbox-1/release" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, RenewInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunCommand(context.Background(), sandboxcontract.CommandRequest{
		Workspace: sandboxcontract.WorkspaceRef{ID: "ws-1"},
		Command:   execmodel.Command{Command: "echo ok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if downloaded || len(result.OutputObjects) != 1 || result.OutputObjects[0].ID != "artifact-1" {
		t.Fatalf("downloaded=%v output_objects=%+v", downloaded, result.OutputObjects)
	}
}

func TestRunCommandWithLeaseLifecyclePollsDownloadsAndReleases(t *testing.T) {
	var gotLease leaseRequest
	var gotJob execJobRequest
	released := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sandboxes:lease" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&gotLease); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(sandboxLease{
				SandboxID:      "sandbox-1",
				LeaseID:        "lease-1",
				RuntimeProfile: "office-basic",
				Status:         "leased",
				ExpiresAt:      time.Now().Add(time.Minute),
			})
		case r.URL.Path == "/v1/jobs" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&gotJob); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(jobResult{JobID: "job-1", SandboxID: "sandbox-1", Status: "running"})
		case r.URL.Path == "/v1/jobs/job-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(jobResult{
				JobID:       "job-1",
				SandboxID:   "sandbox-1",
				WorkspaceID: "ws-1",
				Status:      "succeeded",
				ExitCode:    0,
				Stdout:      "generated",
				OutputArtifacts: []artifactRecord{{
					ArtifactID:  "artifact-1",
					WorkspaceID: "ws-1",
					JobID:       "job-1",
					Name:        "hello.txt",
					Size:        5,
					MIME:        "text/plain",
				}},
			})
		case r.URL.Path == "/v1/sandboxes/sandbox-1/release" && r.Method == http.MethodPost:
			released = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{
		BaseURL:       server.URL,
		RenewInterval: time.Hour,
		PollStart:     time.Millisecond,
		PollMax:       time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunCommand(context.Background(), sandboxcontract.CommandRequest{
		Workspace: sandboxcontract.WorkspaceRef{ID: "ws-1"},
		Command:   execmodel.Command{Command: "python make_file.py", Cwd: "/workspace", Shell: execmodel.ShellSh},
		Sandbox: execmodel.SandboxProfile{
			Provider:       "genesis-sandbox",
			RuntimeProfile: execmodel.RuntimeProfileOfficeBasic,
			TaskType:       execmodel.SandboxTaskOffice,
			Operation:      execmodel.SandboxOperationGenerateDocx,
			Language:       "python",
		},
		Options: execcontract.RunOptions{Timeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotLease.WorkspaceID != "ws-1" || gotLease.RuntimeProfile != "office-basic" || gotLease.TaskType != "office" {
		t.Fatalf("lease=%+v", gotLease)
	}
	if gotJob.SandboxID != "sandbox-1" || gotJob.WorkspaceID != "ws-1" {
		t.Fatalf("job=%+v", gotJob)
	}
	if !released {
		t.Fatal("sandbox was not released")
	}
	if result.Stdout != "generated" || len(result.OutputObjects) != 1 || result.OutputObjects[0].ID != "artifact-1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestRunCommandWarnsWhenReleaseFallsBackToDestroy(t *testing.T) {
	destroyed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sandboxes:lease" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(sandboxLease{SandboxID: "sandbox-1", LeaseID: "lease-1", Status: "leased"})
		case r.URL.Path == "/v1/jobs" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(jobResult{JobID: "job-1", SandboxID: "sandbox-1", Status: "succeeded"})
		case r.URL.Path == "/v1/jobs/job-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(jobResult{JobID: "job-1", SandboxID: "sandbox-1", Status: "succeeded", ExitCode: 0, Stdout: "ok"})
		case r.URL.Path == "/v1/sandboxes/sandbox-1/release" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(errorResponse{Code: "RELEASE_FAILED", Message: "release failed"})
		case r.URL.Path == "/v1/sandboxes/sandbox-1" && r.Method == http.MethodDelete:
			destroyed = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, RenewInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RunCommand(context.Background(), sandboxcontract.CommandRequest{
		Command: execmodel.Command{Command: "echo ok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !destroyed {
		t.Fatal("sandbox was not destroyed after release failure")
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "release失败") {
		t.Fatalf("warnings=%+v", result.Warnings)
	}
}

func TestRunCommandMapsRuntimeUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(errorResponse{Code: "RUNTIME_UNAVAILABLE", Message: "docker down"})
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunCommand(context.Background(), sandboxcontract.CommandRequest{
		Command: execmodel.Command{Command: "echo ok"},
		Sandbox: execmodel.SandboxProfile{
			RuntimeProfile: execmodel.RuntimeProfileCodePolyglotBasic,
			TaskType:       execmodel.SandboxTaskShell,
			Operation:      execmodel.SandboxOperationRunShell,
		},
	})
	if execcontract.CodeOf(err) != execcontract.ErrCodeSandboxUnavailable {
		t.Fatalf("err=%v code=%s", err, execcontract.CodeOf(err))
	}
}

func TestRunCommandMapsInvalidArgument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse{Code: "INVALID_ARGUMENT", Message: "bad request"})
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RunCommand(context.Background(), sandboxcontract.CommandRequest{
		Command: execmodel.Command{Command: "echo ok"},
	})
	if execcontract.CodeOf(err) != execcontract.ErrCodeInvalidInput {
		t.Fatalf("err=%v code=%s", err, execcontract.CodeOf(err))
	}
}
