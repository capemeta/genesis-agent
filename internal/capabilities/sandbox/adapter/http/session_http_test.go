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

func TestOpenSessionIgnoresCallerSandboxIDMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(sessionRecord{
				SessionID: "session-1", WorkspaceID: "ws-1",
				RuntimeProfile: "office-basic", StatePolicy: "session",
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
		Workspace: sandboxcontract.WorkspaceRef{
			ID: "ws-1",
			Metadata: map[string]string{
				"sandbox_id": "stale-sandbox",
				"extra":      "keep",
			},
		},
		Sandbox: execmodel.SandboxProfile{RuntimeProfile: execmodel.RuntimeProfileOfficeBasic},
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := session.Workspace()
	if workspace.Metadata["sandbox_id"] != "" {
		t.Fatalf("dormant Workspace leaked caller sandbox_id: %+v", workspace.Metadata)
	}
	if workspace.Metadata["extra"] != "keep" {
		t.Fatalf("non-identity metadata dropped: %+v", workspace.Metadata)
	}
	_ = session.Close(context.Background())
}
