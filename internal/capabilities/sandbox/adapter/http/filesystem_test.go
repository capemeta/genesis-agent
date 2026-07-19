package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
)

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

func TestStatMapsSandboxNotFoundCodeToNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(errorResponse{Code: "SANDBOX_NOT_FOUND", Message: "NOT_FOUND: workspace path a.txt"})
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Stat(context.Background(), sandboxcontract.FileRequest{
		Workspace: sandboxcontract.WorkspaceRef{ID: "session-1"},
		Path:      fsmodel.ResolvedPath{WorkspaceRel: "a.txt"},
	})
	if fscontract.CodeOf(err) != fscontract.ErrCodeNotFound {
		t.Fatalf("err=%v code=%s", err, fscontract.CodeOf(err))
	}
}
